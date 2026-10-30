//go:build linux

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestVerifierCancelsChildProcessGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & echo $!; wait")
	if e := configureTestProcess(c); e != nil {
		t.Fatal(e)
	}
	var output bytes.Buffer
	c.Stdout = &output
	started := time.Now()
	if e := c.Run(); e == nil {
		t.Fatal("expected cancellation")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("cancellation did not join")
	}
	pid, e := strconv.Atoi(strings.TrimSpace(output.String()))
	if e != nil {
		t.Fatal(e)
	}
	// A minimal container PID 1 may retain a terminated zombie; it is not running.
	data, e := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if os.IsNotExist(e) {
		return
	}
	if e != nil {
		t.Fatal(e)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 || fields[2] != "Z" {
		t.Fatal("child still running", string(data))
	}
}
