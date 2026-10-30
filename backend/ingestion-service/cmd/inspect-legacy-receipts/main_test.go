package main

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	store "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/mysql"
	"github.com/google/uuid"
)

func fixture(t *testing.T) (store.LegacyImport, inventory) {
	t.Helper()
	tenant := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	table := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	key := "secret-key"
	model := ingestion.PublishedDataModel{TenantID: tenant, RevisionID: "revision", Writable: true, RecordLookupField: "object_id", Tables: map[string]ingestion.ObjectSchema{"records": {ID: table, Name: "records", Fields: map[string]ingestion.FieldSchema{"object_id": {Name: "object_id", DataType: "String"}}}}}
	raw := []byte(`{"object_id":"private-object"}`)
	n, id, violations := ingestion.ValidateRecord(model, "records", map[string]any{"object_id": "private-object"}, ingestion.ModePatch)
	if len(violations) > 0 {
		t.Fatal(violations)
	}
	payload, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha1.Sum(payload)
	response, _ := json.Marshal(ingestion.RecordResult{ObjectID: id, Action: "created", RevisionID: model.RevisionID})
	in := store.LegacyImport{HistoricalModel: model, ObjectType: "records", Mode: ingestion.ModePatch, RawJSON: raw,
		Receipt: ingestion.IdempotencyKey{TenantID: tenant.String(), Key: key, RequestHash: hex.EncodeToString(hash[:]), ResponseKind: "single", ResponsePayload: response, CreatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
		Audits:  []ingestion.IngestionAudit{{TenantID: tenant.String(), ObjectType: "records", ObjectID: id, Mode: ingestion.ModePatch, RevisionID: model.RevisionID, Status: "succeeded", Payload: payload, IdempotencyKey: &key}}}
	if err := store.ValidateLegacyEvidence(in, 1<<20); err != nil {
		t.Fatal(err)
	}
	return in, inventory{Version: 1, Snapshot: "approved-chunk", Tenant: tenant.String(), Table: table.String(), Object: "records"}
}

func wire(t *testing.T, in store.LegacyImport) string {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func TestOfflineEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		mutate     func(*store.LegacyImport)
	}{
		{"valid", "", func(*store.LegacyImport) {}},
		{"raw_missing", "evidence_rejected", func(in *store.LegacyImport) { in.RawJSON = nil }},
		{"hash_mismatch", "evidence_rejected", func(in *store.LegacyImport) { in.Receipt.RequestHash = strings.Repeat("0", 40) }},
		{"audit_missing", "evidence_rejected", func(in *store.LegacyImport) { in.Audits = nil }},
		{"tenant_mismatch", "scope_mismatch", func(in *store.LegacyImport) { in.Receipt.TenantID = uuid.NewString() }},
		{"table_mismatch", "scope_mismatch", func(in *store.LegacyImport) { in.ObjectType = "other" }},
		{"key_bound", "invalid_key_bound", func(in *store.LegacyImport) { in.Receipt.Key = strings.Repeat("x", 1025) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, scope := fixture(t)
			tc.mutate(&in)
			data := wire(t, in)
			got, err := inspect(strings.NewReader(data), digest(data), scope, 1<<20, 1)
			if err != nil {
				t.Fatal(err)
			}
			if tc.code == "" {
				if got.Valid != 1 || got.RawInput != 1 || got.HistoricalSchema != 1 || got.Mode != 1 || got.Audits != 1 || got.Results != 1 {
					t.Fatal(got)
				}
			} else if len(got.Findings) != 1 || got.Findings[0].Code != tc.code || got.Valid != 0 {
				t.Fatal(got)
			}
			encoded, _ := json.Marshal(got)
			if bytes.Contains(encoded, []byte("secret-key")) || bytes.Contains(encoded, []byte("private-object")) {
				t.Fatal("source data leaked")
			}
		})
	}
}

func TestOfflineBoundsAndDeterminism(t *testing.T) {
	in, scope := fixture(t)
	one := wire(t, in)
	for _, data := range []string{"{} {}\n", "null\n", "{\"unknown\":true}\n", "\xff\n", "\n"} {
		got, err := inspect(strings.NewReader(data), digest(data), scope, 1<<20, 10)
		if err != nil || len(got.Findings) != 1 {
			t.Fatal(got, err)
		}
	}
	two := one + one
	a, err := inspect(strings.NewReader(two), digest(two), scope, 1<<20, 2)
	if err != nil || a.Valid != 1 || len(a.Findings) != 1 || a.Findings[0].Code != "duplicate_receipt_key" {
		t.Fatal(a, err)
	}
	b, err := inspect(strings.NewReader(two), digest(two), scope, 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if !bytes.Equal(aa, bb) {
		t.Fatal("nondeterministic inventory")
	}
	for _, tc := range []struct {
		data, hash string
		rows       int
	}{{two, digest(two), 1}, {one, strings.Repeat("0", 64), 1}, {"", digest(""), 1}, {strings.Repeat("x", (16<<20)+2), "", 1}} {
		if _, err := inspect(strings.NewReader(tc.data), tc.hash, scope, 1<<20, tc.rows); err == nil {
			t.Fatal("accepted invalid input")
		}
	}
	got, err := inspect(strings.NewReader(one), digest(one), scope, 1, 1)
	if err != nil || got.Findings[0].Code != "evidence_limit" {
		t.Fatal(got, err)
	}
}

func TestOfflineCLI(t *testing.T) {
	in, scope := fixture(t)
	data := wire(t, in)
	path := filepath.Join(t.TempDir(), "input.jsonl")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-input", path, "-snapshot", scope.Snapshot, "-sha256", digest(data), "-tenant", scope.Tenant, "-table", scope.Table, "-object-type", scope.Object}
	var out bytes.Buffer
	if err := run(args, &out); err != nil {
		t.Fatal(err)
	}
	var got inventory
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Valid != 1 {
		t.Fatal(got, err)
	}
	out.Reset()
	if err := run(append(args, "-sha256", strings.Repeat("0", 64)), &out); err == nil || out.Len() != 0 {
		t.Fatal("checksum failure emitted report", err)
	}
	in.RawJSON = nil
	data = wire(t, in)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run(append(args, "-sha256", digest(data)), &out); err == nil || out.Len() == 0 {
		t.Fatal("exceptions must emit report and fail", err)
	}
}
