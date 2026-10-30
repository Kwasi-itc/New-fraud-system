package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlrelease"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql/metadata"
)

func TestReleaseCommandFailsClosedBeforeNetworkOrActivation(t *testing.T) {
	for _, args := range [][]string{
		{"--mode", "activate"},
		{"--mode", "down"},
		{"--mode", "force"},
		{"--mode", "verify", "unexpected"},
		{"--mode", "apply", "--allow-migrate-database", "another_database"},
		{"--mode", "artifact", "--artifact", "/no/release/artifact"},
	} {
		var output bytes.Buffer
		if err := run(args, &output); err == nil || strings.Contains(output.String(), "PASS:") {
			t.Fatal("unsafe release command reported success", args, err, output.String())
		}
	}
}

func TestReleaseCommandOfflineBindingAndUnstampedDeploymentDenial(t *testing.T) {
	// Synthetic shapes are only command parsing fixtures, not release evidence.
	id := mysqlrelease.Identity{Database: "release_fixture", Definer: "de_release", MySQLVersion: "8.4.8", Revision: strings.Repeat("a", 40), SourceSHA256: strings.Repeat("b", 64), ImageDigest: "sha256:" + strings.Repeat("c", 64)}
	steps, err := metadata.DispatchStatements(id.Database, id.Definer)
	if err != nil {
		t.Fatal(err)
	}
	latest := map[string]string{}
	for i := range steps {
		key := steps[i].Kind + ":" + steps[i].Name
		steps[i].Before = latest[key]
		raw, _ := json.Marshal(map[string]string{"object": steps[i].Name, "sql": steps[i].SQL})
		steps[i].After = string(raw)
		latest[key] = string(raw)
	}
	raw, err := json.Marshal(mysqlrelease.Artifact{Format: 1, Identity: id, Steps: steps})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	sum := sha256.Sum256(raw)
	path := filepath.Join(t.TempDir(), "release.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	base := []string{"--artifact", path, "--sha256", hex.EncodeToString(sum[:]), "--database", id.Database, "--definer", id.Definer, "--revision", id.Revision, "--source-sha256", id.SourceSHA256, "--image", id.ImageDigest}
	var out bytes.Buffer
	if err := run(append(append([]string{}, base...), "--mode", "artifact"), &out); err != nil || !strings.Contains(out.String(), "activation remains blocked") {
		t.Fatal(err, out.String())
	}
	out.Reset()
	if err := run(append(append([]string{}, base...), "--mode", "verify"), &out); err == nil || !strings.Contains(err.Error(), "build provenance") {
		t.Fatal("unstamped installer allowed deployment", err)
	}
	previousRevision, previousSHA := builtRevision, builtSourceSHA256
	builtRevision, builtSourceSHA256 = id.Revision, id.SourceSHA256
	t.Cleanup(func() { builtRevision, builtSourceSHA256 = previousRevision, previousSHA })
	if err := run(append(append([]string{}, base...), "--mode", "apply"), &out); err == nil || !strings.Contains(err.Error(), "unsupported operation") {
		t.Fatal("unapproved production writes exposed", err)
	}
}
