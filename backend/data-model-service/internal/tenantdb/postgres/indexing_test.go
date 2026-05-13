package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

type capturingExecutor struct {
	queries []string
}

func (e *capturingExecutor) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	e.queries = append(e.queries, sql)
	return pgconn.CommandTag{}, nil
}

func (e *capturingExecutor) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}

func (e *capturingExecutor) QueryRow(context.Context, string, ...any) pgx.Row {
	return nil
}

func TestCreateUniqueIndexDoesNotSchemaQualifyIndexName(t *testing.T) {
	exec := &capturingExecutor{}
	manager := NewSchemaManager(exec)
	tenantRecord := tenant.Tenant{
		ID:         uuid.MustParse("cc2d0bdc-33e7-452d-88ba-2f80762a03d1"),
		Name:       "Fraud Ops",
		SchemaName: "tenant_cc2d0bdc33e7452d88ba2f80762a03d1",
		Status:     tenant.StatusActive,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	table := datamodel.Table{
		ID:       uuid.New(),
		TenantID: tenantRecord.ID,
		Name:     "transactions",
	}

	if err := manager.CreateUniqueIndex(context.Background(), tenantRecord, table, []string{"object_id"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(exec.queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(exec.queries))
	}

	query := exec.queries[0]
	if strings.Contains(query, `"tenant_cc2d0bdc33e7452d88ba2f80762a03d1"."uniq_`) {
		t.Fatalf("expected unqualified index name, got %s", query)
	}
	if !strings.Contains(query, `ON "tenant_cc2d0bdc33e7452d88ba2f80762a03d1"."transactions"`) {
		t.Fatalf("expected schema-qualified table target, got %s", query)
	}
}
