// verify-mysql-fixture runs selected acceptance suites against the explicit
// disposable Compose endpoint. It never starts a service or migrates an app DB.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type event struct{ Action, Test, Package string }
type summary struct {
	Discovered, Executed, Passed, Failed, Skipped int
	Error                                         string `json:",omitempty"`
}

// Keep selection shared between discovery and execution. Operational acceptance
// is scoped to ingestion; it cannot certify unrelated shared queue consumers.
func suitePattern(suite string) (string, error) {
	switch suite {
	case "":
		return "^TestFullShared", nil
	case "process":
		return "^TestProcess", nil
	case "queue":
		return "^Test(IngestionWork|WorkOperator|WorkRedrive|WorkInspection|WorkTelemetry|FullSharedRedrive|FullSharedDeferredMetrics)", nil
	default:
		return "", errors.New("unknown verification suite")
	}
}

func summarize(tests []string, events []event) (summary, error) {
	s := summary{Discovered: len(tests)}
	known := map[string]bool{}
	runs := map[string]int{}
	passes := map[string]int{}
	for _, name := range tests {
		if known[name] {
			return s, errors.New("duplicate discovered test")
		}
		known[name] = true
	}
	for _, e := range events {
		if e.Action == "skip" {
			s.Skipped++
		}
		if e.Action == "fail" {
			s.Failed++
		}
		if e.Test == "" || strings.Contains(e.Test, "/") {
			continue
		}
		if !known[e.Test] {
			return s, fmt.Errorf("undiscovered test: %s", e.Test)
		}
		if e.Action == "run" {
			runs[e.Test]++
			s.Executed++
		}
		if e.Action == "pass" {
			passes[e.Test]++
			s.Passed++
		}
	}
	if len(tests) == 0 || s.Skipped != 0 || s.Failed != 0 {
		return s, errors.New("empty, skipped or failed required suite")
	}
	for _, name := range tests {
		if runs[name] != 1 || passes[name] != 1 {
			return s, fmt.Errorf("test did not run/pass exactly once: %s", name)
		}
	}
	return s, nil
}
func run(ctx context.Context, directory string) error {
	if os.Getenv("INGESTION_MYSQL_TEST_ADDRESS") != "mysql:3306" || os.Getenv("INGESTION_REQUIRE_MYSQL_TEST") != "1" || os.Getenv("INGESTION_DATA_MODEL_FIXTURE_EXPORTER") == "" {
		return errors.New("explicit isolated MySQL and owner exporter required")
	}
	pattern, err := suitePattern(os.Getenv("INGESTION_VERIFY_SUITE"))
	if err != nil {
		return err
	}
	if os.Getenv("INGESTION_VERIFY_SUITE") == "" && os.Getenv("INGESTION_IMPORT_TEST_BINARY") == "" {
		return errors.New("full-schema suite requires compiled importer via INGESTION_IMPORT_TEST_BINARY")
	}
	selection := regexp.MustCompile(pattern)
	output, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	if err = os.Mkdir(output, 0700); err != nil {
		return fmt.Errorf("new output directory required: %w", err)
	}
	args := []string{"test", "-mod=readonly", "-list", pattern, "./internal/store/mysql"}
	discovered, err := exec.CommandContext(ctx, "go", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("discover suite: %w: %s", err, discovered)
	}
	var tests []string
	for _, line := range strings.Split(string(discovered), "\n") {
		line = strings.TrimSpace(line)
		if selection.MatchString(line) {
			tests = append(tests, line)
		}
	}
	if len(tests) == 0 {
		return errors.New("no required suite tests discovered")
	}
	raw, err := os.OpenFile(filepath.Join(output, "raw.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer raw.Close()
	args = []string{"test", "-mod=readonly", "-json", "-race", "-count=1", "-timeout=" + (time.Duration(len(tests))*45*time.Second + time.Minute).String(), "./internal/store/mysql", "-run", pattern}
	command := exec.CommandContext(ctx, "go", args...)
	if err = configureTestProcess(command); err != nil {
		return err
	}
	command.Env = append(os.Environ(), "INGESTION_FULL_ARTIFACT_DIR="+output)
	stderr, err := os.OpenFile(filepath.Join(output, "stderr.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer stderr.Close()
	command.Stderr = io.MultiWriter(os.Stderr, stderr)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err = command.Start(); err != nil {
		return err
	}
	decoder := json.NewDecoder(io.TeeReader(stdout, raw))
	var events []event
	var decodeErr error
	for {
		var e event
		err = decoder.Decode(&e)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			decodeErr = err
			break
		}
		events = append(events, e)
	}
	if decodeErr != nil {
		_ = command.Process.Kill()
	}
	runErr := command.Wait()
	s, checkErr := summarize(tests, events)
	result := errors.Join(runErr, decodeErr, checkErr)
	if result != nil {
		s.Error = result.Error()
	}
	data, err := json.MarshalIndent(struct {
		Summary        summary
		Tests, Command []string
	}{s, tests, append([]string{"go"}, args...)}, "", "  ")
	if err != nil {
		return errors.Join(result, err)
	}
	err = os.WriteFile(filepath.Join(output, "summary.json"), append(data, '\n'), 0600)
	fmt.Printf("discovered=%d executed=%d passed=%d failed=%d skipped=%d output=%s\n", s.Discovered, s.Executed, s.Passed, s.Failed, s.Skipped, output)
	return errors.Join(result, err)
}
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "new result directory required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := run(ctx, os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
