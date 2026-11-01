package mysqlrelease

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql/metadata"
)

// Synthetic definitions test artifact validation ONLY; integration tests below
// obtain real pinned-server shapes on an independent disposable reference.
func unitArtifact(t *testing.T) Artifact {
	t.Helper()
	i := Identity{"decision_release_test", "de_release", "8.4.8", strings.Repeat("a", 40), strings.Repeat("b", 64), "sha256:" + strings.Repeat("c", 64)}
	steps, err := metadata.DispatchStatements(i.Database, i.Definer)
	if err != nil {
		t.Fatal(err)
	}
	latest := map[string]string{}
	for n := range steps {
		key := steps[n].Kind + ":" + steps[n].Name
		steps[n].Before = latest[key]
		definition, err := json.Marshal(map[string]string{"object": steps[n].Name, "sql": steps[n].SQL})
		if err != nil {
			t.Fatal(err)
		}
		steps[n].After = string(definition)
		latest[key] = steps[n].After
	}
	return Artifact{1, i, steps}
}

func encodeArtifact(t *testing.T, a Artifact) ([]byte, string) {
	t.Helper()
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}

func TestReleasePinnedIdentityCanonicalBytesAndSource(t *testing.T) {
	a := unitArtifact(t)
	raw, sum := encodeArtifact(t, a)
	r, err := Decode(bytes.NewReader(raw), sum, a.Identity)
	if err != nil {
		t.Fatal(err)
	}
	p := r.Plan()
	p.Steps[0].SQL = "DROP TABLE another_owner"
	if r.Plan().Steps[0].SQL == p.Steps[0].SQL || r.Digest() != sum {
		t.Fatal("mutable verified plan")
	}
	for _, test := range []struct {
		name   string
		mutate func(*Artifact)
	}{
		{"sql", func(a *Artifact) { a.Steps[0].SQL = "CREATE TABLE other_owner(id INT)" }},
		{"prefix", func(a *Artifact) { a.Steps = a.Steps[:145] }},
		{"chain", func(a *Artifact) { a.Steps[148].Before = `{"x":"y","z":"a"}` }},
		{"root", func(a *Artifact) { a.Definer = "root" }},
		{"version", func(a *Artifact) { a.MySQLVersion = "9.0" }},
		{"image", func(a *Artifact) { a.ImageDigest = "latest" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := unitArtifact(t)
			test.mutate(&b)
			raw, sum := encodeArtifact(t, b)
			if _, err := Decode(bytes.NewReader(raw), sum, a.Identity); err == nil {
				t.Fatal("unreviewed artifact accepted")
			}
		})
	}
	if _, err := Decode(bytes.NewReader(raw), strings.Repeat("0", 64), a.Identity); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(`"format":1`), []byte(`"format":0,"format":1`), 1),
		append(append([]byte{}, raw...), []byte(`{}`)...),
		bytes.Replace(raw, []byte(`"format":1`), []byte(`"unknown":true,"format":1`), 1),
		append([]byte{' '}, raw...),
	} {
		hash := sha256.Sum256(invalid)
		if _, err := Decode(bytes.NewReader(invalid), hex.EncodeToString(hash[:]), a.Identity); err == nil {
			t.Fatal("ambiguous artifact accepted")
		}
	}
}

func TestOwnerGrantsExhaustiveAndNoPrivilegeExpansion(t *testing.T) {
	for _, role := range []string{"api", "worker"} {
		grants, err := OwnerGrants("fraud", "de_"+role, "127.0.0.1", role)
		if err != nil {
			t.Fatal(err)
		}
		if len(grants) < 35 {
			t.Fatal("incomplete owner inventory")
		}
		for _, grant := range grants {
			for _, forbidden := range []string{"*", "GRANT OPTION", "TRIGGER", "EXECUTE", "CREATE", "DROP", "ALTER", "exact_key", " ON mysql.", " ON `fraud`.auth"} {
				if strings.Contains(grant, forbidden) {
					t.Fatal("privilege expansion", grant)
				}
			}
			if strings.Contains(grant, "schema_migrations") && !strings.HasPrefix(grant, "GRANT SELECT ON ") {
				t.Fatal("runtime can alter migration history")
			}
			if strings.Contains(grant, "DELETE") && role == "worker" && !strings.Contains(grant, "job_deliveries") {
				t.Fatal("worker can erase permanent evidence")
			}
		}
	}
	for _, host := range []string{"", "%", "10.%", "localhost' OR 1=1", "host\\name"} {
		if _, err := OwnerGrants("fraud", "de_api", host, "api"); err == nil {
			t.Fatal("unsafe account host", host)
		}
	}
	if _, err := OwnerGrants("fraud", "root", "localhost", "api"); err == nil {
		t.Fatal("root runtime account")
	}
}
