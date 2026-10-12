package datamodel

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/google/uuid"
	"testing"
)

type contractStoreFunc func(context.Context, uuid.UUID, string, uuid.UUID, uuid.UUID) (ports.PublishedContract, error)

func (f contractStoreFunc) ReadContract(ctx context.Context, tenant uuid.UUID, table string, knownTable, knownVersion uuid.UUID) (ports.PublishedContract, error) {
	return f(ctx, tenant, table, knownTable, knownVersion)
}

func TestContractReaderVersionStatusFailureAndIsolation(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	table := uuid.New()
	version := uuid.New()
	status := "active"
	var fail error
	calls := 0
	payloads := 0
	store := contractStoreFunc(func(_ context.Context, id uuid.UUID, name string, knownTable, knownVersion uuid.UUID) (ports.PublishedContract, error) {
		calls++
		if fail != nil {
			return ports.PublishedContract{}, fail
		}
		state := ports.PublishedContract{TenantID: id, TableID: table, TableName: name, Version: version, TenantStatus: status, SchemaName: "tenant_schema"}
		if knownTable != table || knownVersion != version {
			payloads++
			state.Payload, _ = json.Marshal(map[string]any{"id": table, "name": name, "fields": map[string]any{"amount": map[string]any{"name": "amount", "data_type": "float", "nullable": false}}})
		}
		return state, nil
	})
	reader := NewContractReader(store, nil, 4, 10000)
	first, err := reader.GetPublishedTable(ctx, tenant, "records")
	if err != nil {
		t.Fatal(err)
	}
	delete(first.Tables["records"].Fields, "amount")
	hit, err := reader.GetPublishedTable(ctx, tenant, "records")
	if err != nil || len(hit.Tables["records"].Fields) != 1 || payloads != 1 {
		t.Fatal("cache copy/version failure")
	}
	status = "disabled"
	disabled, err := reader.GetPublishedTable(ctx, tenant, "records")
	if err != nil || disabled.Writable || disabled.RevisionID == hit.RevisionID || payloads != 1 {
		t.Fatal("tenant status not checked on warm read")
	}
	version = uuid.New()
	if _, err := reader.GetPublishedTable(ctx, tenant, "records"); err != nil || payloads != 2 {
		t.Fatal("changed contract not loaded")
	}
	if _, err := reader.GetPublishedTable(ctx, tenant, "accounts"); err != nil || payloads != 3 {
		t.Fatal("cross-table cache")
	}
	if _, err := reader.GetPublishedTable(ctx, uuid.New(), "records"); err != nil || payloads != 4 {
		t.Fatal("cross-tenant cache")
	}
	fail = errors.New("database unavailable")
	if _, err := reader.GetPublishedTable(ctx, tenant, "records"); !errors.Is(err, fail) {
		t.Fatal("dependency error must fail closed")
	}
	if calls != 7 {
		t.Fatalf("expected one authoritative read each time, got %d", calls)
	}
}

func TestContractReaderRejectsInvalidResponses(t *testing.T) {
	for _, test := range []string{"tenant", "version", "payload", "uncached"} {
		t.Run(test, func(t *testing.T) {
			id := uuid.New()
			table := uuid.New()
			store := contractStoreFunc(func(_ context.Context, _ uuid.UUID, _ string, _, _ uuid.UUID) (ports.PublishedContract, error) {
				state := ports.PublishedContract{TenantID: id, TableID: table, TableName: "records", Version: uuid.New(), TenantStatus: "active"}
				switch test {
				case "tenant":
					state.TenantID = uuid.New()
				case "version":
					state.Version = uuid.Nil
				case "payload":
					state.Payload = []byte(`{}`)
				}
				return state, nil
			})
			if _, err := NewContractReader(store, nil, 4, 10000).GetPublishedTable(context.Background(), id, "records"); err == nil {
				t.Fatal("invalid contract accepted")
			}
		})
	}
}
