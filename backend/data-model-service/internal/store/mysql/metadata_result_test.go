package mysql

import (
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
	"testing"
	"time"
)

// These encodings are durable response contracts, independent of later domain
// edits. Changing them requires reviewing a new result kind and old-row reader.
func TestMetadataResultWireContract(t *testing.T) {
	id := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	tenant := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	field := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	at := time.Date(2026, 10, 28, 0, 0, 0, 123456000, time.UTC)
	table := datamodel.Table{ID: id, TenantID: tenant, Name: "records", Description: "description", Alias: "alias", SemanticType: "customer", CreatedAt: at, UpdatedAt: at}
	col := datamodel.Field{ID: field, TenantID: tenant, TableID: id, Name: "value", DataType: datamodel.DataTypeString, Nullable: true, CreatedAt: at, UpdatedAt: at}
	for _, tc := range []struct {
		kind  string
		value any
		want  string
		id    uuid.UUID
	}{
		{tableResultKind, table, `{"ID":"11111111-1111-4111-8111-111111111111","TenantID":"22222222-2222-4222-8222-222222222222","Name":"records","Description":"description","Alias":"alias","SemanticType":"customer","CaptionField":"","Archived":false,"CreatedAt":"2026-10-28T00:00:00.123456Z","UpdatedAt":"2026-10-28T00:00:00.123456Z"}`, id},
		{fieldResultKind, col, `{"ID":"33333333-3333-4333-8333-333333333333","TenantID":"22222222-2222-4222-8222-222222222222","TableID":"11111111-1111-4111-8111-111111111111","Name":"value","Description":"","Alias":"","DataType":"string","Nullable":true,"IsEnum":false,"IsUnique":false,"Archived":false,"CreatedAt":"2026-10-28T00:00:00.123456Z","UpdatedAt":"2026-10-28T00:00:00.123456Z"}`, field},
	} {
		raw, err := json.Marshal(tc.value)
		if err != nil || string(raw) != tc.want {
			t.Fatal("result encoding changed", tc.kind, string(raw), err)
		}
		result := metadataResult{kind: tc.kind, id: tc.id, raw: raw}
		if err = result.decode(tenant, id); err != nil {
			t.Fatal(err)
		}
		if err = result.decode(uuid.New(), id); err == nil {
			t.Fatal("foreign tenant result")
		}
		if err = result.decode(tenant, uuid.New()); err == nil {
			t.Fatal("foreign table result")
		}
		result.id = uuid.New()
		if err = result.decode(tenant, id); err == nil {
			t.Fatal("foreign resource result")
		}
	}
}
