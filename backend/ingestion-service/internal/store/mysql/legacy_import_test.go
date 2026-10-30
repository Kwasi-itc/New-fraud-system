package mysql

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func legacyFixture(t *testing.T, f recordFixtureState, kind string) LegacyImport {
	t.Helper()
	raw := []byte(`{"object_id":"legacy","number":7.0}`)
	if kind == "batch" {
		raw = []byte(`[{"object_id":"legacy","number":7.0},{"object_id":"second","number":8}]`)
	}
	var old []map[string]any
	if kind == "single" {
		var r map[string]any
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatal(err)
		}
		old = []map[string]any{r}
	} else {
		if err := json.Unmarshal(raw, &old); err != nil {
			t.Fatal(err)
		}
	}
	in := LegacyImport{HistoricalModel: f.model, ObjectType: "records", Mode: ingestion.ModePatch, RawJSON: raw}
	in.HistoricalModel.ManagedSystemFields = []string{"updated_at"}
	key := "old-key"
	var results []ingestion.RecordResult
	var hashed any = old
	for _, record := range old {
		n, id, failures := ingestion.ValidateRecord(in.HistoricalModel, "records", record, in.Mode)
		if len(failures) > 0 {
			t.Fatal(failures)
		}
		payload, err := json.Marshal(n)
		if err != nil {
			t.Fatal(err)
		}
		in.Audits = append(in.Audits, ingestion.IngestionAudit{ID: uuid.NewString(), TenantID: f.model.TenantID.String(), ObjectType: "records", ObjectID: id, Mode: in.Mode, RevisionID: f.model.RevisionID, Status: "succeeded", Payload: payload, ValidationError: []byte("[]"), IdempotencyKey: &key, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)})
		results = append(results, ingestion.RecordResult{ObjectID: id, Action: "created", RevisionID: f.model.RevisionID})
		if kind == "single" {
			hashed = n
		}
	}
	body, err := json.Marshal(hashed)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha1.Sum(body)
	var response []byte
	if kind == "single" {
		response, err = json.Marshal(results[0])
	} else {
		response, err = json.Marshal(results)
	}
	if err != nil {
		t.Fatal(err)
	}
	in.Receipt = ingestion.IdempotencyKey{TenantID: f.model.TenantID.String(), Key: key, RequestHash: hex.EncodeToString(hash[:]), ResponseKind: kind, ResponsePayload: response, CreatedAt: time.Date(2020, 2, 3, 4, 5, 6, 123456000, time.FixedZone("source", 3600))}
	return in
}

func TestLegacyReceiptTranslation(t *testing.T) {
	for _, kind := range []string{"single", "batch"} {
		t.Run(kind, func(t *testing.T) {
			f, o, _ := ownerFixture(t)
			ddl, err := LegacyImportSchema(f.database)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(ddl); err != nil {
				t.Fatal(err)
			}
			importer, err := NewLegacyImporter(o, func(context.Context, *sql.Tx, ReceiptRequest) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			denied, err := NewLegacyImporter(o, func(context.Context, *sql.Tx, ReceiptRequest) error { return ErrOwnerAuthorization })
			if err != nil {
				t.Fatal(err)
			}
			in := legacyFixture(t, f, kind)
			if _, err := denied.Import(context.Background(), in); !errors.Is(err, ErrOwnerAuthorization) {
				t.Fatal(err)
			}
			assertOwnerCounts(t, f, 0, 0, 0, 0)
			command, expected, _, err := translateLegacy(in, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			r, err := importer.Import(context.Background(), in)
			if err != nil || r.Replayed || string(r.Payload) != string(expected) {
				t.Fatal(r, err)
			}
			r, err = importer.Import(context.Background(), in)
			if err != nil || !r.Replayed {
				t.Fatal(r, err)
			}
			var created mysqlstore.DateTime
			if err := f.db.QueryRow("SELECT created_at FROM "+o.receipts.receiptTable+" WHERE tenant_id=? AND request_key=?", mysqlstore.UUID(o.receipts.tenant), []byte(in.Receipt.Key)).Scan(&created); err != nil || !created.Time.Equal(in.Receipt.CreatedAt) {
				t.Fatal(created, err)
			}
			// Normal public replay retains the source result without physical effects.
			r, err = o.Execute(context.Background(), command, in.Receipt.Key)
			if err != nil || !r.Replayed || string(r.Payload) != string(expected) {
				t.Fatal(r, err)
			}
			assertOwnerCounts(t, f, 0, 0, 0, 1)
			in.Audits[0].ID = uuid.NewString()
			if _, err := importer.Import(context.Background(), in); !errors.Is(err, ErrReceiptConflict) {
				t.Fatal("different evidence accepted", err)
			}
		})
	}
}

func TestLegacyReceiptRejectsMissingOrConflictingEvidence(t *testing.T) {
	f, _, _ := ownerFixture(t)
	for _, mutation := range []func(*LegacyImport){
		func(v *LegacyImport) { v.RawJSON = nil },
		func(v *LegacyImport) { v.Receipt.CreatedAt = time.Time{} },
		func(v *LegacyImport) { v.Receipt.CreatedAt = v.Receipt.CreatedAt.Add(time.Nanosecond) }, func(v *LegacyImport) { v.Audits = nil },
		func(v *LegacyImport) { v.Receipt.RequestHash = "0000000000000000000000000000000000000000" },
		func(v *LegacyImport) { v.Mode = ingestion.ModeCreate }, func(v *LegacyImport) { v.Receipt.TenantID = uuid.NewString() },
		func(v *LegacyImport) { v.Audits[0].ObjectID = "wrong" }, func(v *LegacyImport) { v.Audits[0].Payload = []byte(`{}`) },
		func(v *LegacyImport) { v.Receipt.ResponsePayload = []byte(`{"object_id":"wrong"}`) },
	} {
		in := legacyFixture(t, f, "single")
		mutation(&in)
		if _, _, _, err := translateLegacy(in, 1<<20); err == nil {
			t.Fatal("invalid source evidence accepted")
		}
	}
}
