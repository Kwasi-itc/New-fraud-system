package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func verifyPhysicalPublicationPort(t *testing.T, ctx context.Context, db *sql.DB, database string, tenantID uuid.UUID, physical store.PhysicalTable, original store.PhysicalPublicationRequest, policies []store.PhysicalAccessPolicy, scenario string) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Microsecond)
	jobID := uuid.New()
	denied := errors.New("current permission denied")
	localCalls, preCalls, calls := 0, 0, 0
	queue, err := store.NewIndexDeliveryStore(db, database, "physical_port", 3, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if scenario == "port_missing_queue" {
		queue = nil
	}
	metadata, err := store.NewTransactionManager(db, database, tenantID, 100, 1<<20, func(ctx context.Context, tx *sql.Tx, scope uuid.UUID) error {
		localCalls++
		if scope != tenantID || scenario == "port_permission" {
			return denied
		}
		sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + physical.TableID.String()))
		var owns bool
		if err := tx.QueryRowContext(ctx, "SELECT IS_USED_LOCK(?)=CONNECTION_ID()", fmt.Sprintf("data_model_ddl_%x", sum[:24])).Scan(&owns); err != nil {
			return err
		}
		if !owns {
			return errors.New("local authorization lost pinned lock")
		}
		return nil
	}, queue)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := store.NewPublicationStore(db, database, 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	deny := false
	manager, err := store.NewPhysicalPublicationManager(metadata, publication, physical, policies, func(ctx context.Context, scope uuid.UUID, request ports.PhysicalPublicationRequest) error {
		preCalls++
		if deny || scope != tenantID || request.ActorID != original.ActorID || request.ActorKind != original.ActorKind {
			return denied
		}
		// The one-connection pool proves this callback runs before DB locks.
		return db.PingContext(ctx)
	})
	if err != nil {
		t.Fatal(err)
	}
	// Changes to caller-owned policy slices cannot change configured authority.
	policies[0].Account.Objects[0].Privileges[0] = "DELETE"
	var publisher ports.PhysicalPublicationManager = manager
	request := ports.PhysicalPublicationRequest{TableID: physical.TableID, OperationID: original.Operation.ID, RequestHash: original.Operation.RequestHash, Condition: original.Operation.Condition, ActorKind: original.ActorKind, ActorID: original.ActorID}
	var retained ports.MutationStore
	owner := func(s ports.MutationStore) error {
		calls++
		retained = s
		table, err := s.Tables().GetByID(ctx, physical.TableID)
		if err != nil {
			return err
		}
		table.Alias = "driver-free publication"
		if err := s.Tables().Update(ctx, table); err != nil {
			return err
		}
		if err := s.SchemaChanges().Create(ctx, datamodel.SchemaChange{ID: uuid.New(), TenantID: tenantID, Operation: "physical_port_test", ResourceType: "table", ResourceID: physical.TableID, Status: "applied", Details: []byte(`{}`), CreatedAt: at}); err != nil {
			return err
		}
		if err := s.TenantSchemaMigrations().Create(ctx, datamodel.TenantSchemaMigration{ID: uuid.New(), TenantID: tenantID, Version: jobID.String(), AppliedAt: at}); err != nil {
			return err
		}
		if err := s.IndexJobs().Create(ctx, datamodel.IndexJob{ID: jobID, TenantID: tenantID, TableID: &physical.TableID, TableName: table.Name, IndexType: "search", Columns: []string{"object_id"}, Status: "pending", RequestedAt: at}); err != nil {
			return err
		}
		if err := s.EnqueueIndexJob(ctx, jobID, nil); err != nil {
			return err
		}
		switch scenario {
		case "port_owner_error":
			return errors.New("owner failed after queue")
		case "port_foreign_scope":
			table.TenantID = uuid.New()
			return s.Tables().Update(ctx, table)
		case "port_legacy_ddl":
			return s.SchemaManager().CreateTable(ctx, tenant.Tenant{}, table)
		}
		return nil
	}
	result, err := publisher.Publish(ctx, request, owner)
	if scenario != "port_commit" {
		if err == nil || result.Version != uuid.Nil {
			t.Fatalf("failed publication returned success: %v %v", result, err)
		}
		if scenario == "port_permission" && (!errors.Is(err, denied) || calls != 0) {
			t.Fatalf("permission bypass: %v calls=%d", err, calls)
		}
		if scenario == "port_missing_queue" && !errors.Is(err, ports.ErrJobQueueUnavailable) {
			t.Fatal(err)
		}
	}
	if scenario == "port_commit" {
		if err != nil || result.Version == uuid.Nil || calls != 1 || localCalls != 1 {
			t.Fatalf("publication: %v %v calls=%d local=%d", result, err, calls, localCalls)
		}
		replay, err := publisher.Publish(ctx, request, owner)
		if err != nil || replay.Version != result.Version || calls != 1 || localCalls != 1 || preCalls != 2 {
			t.Fatalf("replay repeated owner effects: %v %v", replay, err)
		}
		deny = true
		if _, err := publisher.Publish(ctx, request, owner); !errors.Is(err, denied) {
			t.Fatalf("replay bypassed current permission: %v", err)
		}
	}
	if retained != nil {
		if _, err := retained.Tables().GetByID(ctx, physical.TableID); !errors.Is(err, sql.ErrTxDone) {
			t.Fatalf("escaped store usable: %v", err)
		}
	}
	wrong := request
	wrong.TableID = uuid.New()
	if _, err := publisher.Publish(ctx, wrong, owner); err == nil {
		t.Fatal("foreign table accepted")
	}
	want := 0
	if scenario == "port_commit" {
		want = 1
	}
	for _, name := range []string{"data_model_schema_change_log", "data_model_tenant_schema_migrations", "data_model_index_jobs", "data_model_index_deliveries", "data_model_publication_operations"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+name+" WHERE tenant_id=?", mysqlstore.UUID(tenantID)).Scan(&count); err != nil || count != want {
			t.Fatalf("%s rows=%d want=%d err=%v", name, count, want, err)
		}
	}
	var alias, gate string
	if err := db.QueryRowContext(ctx, "SELECT alias FROM data_model_tables WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(physical.TableID)).Scan(&alias); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT write_state FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(physical.TableID)).Scan(&gate); err != nil {
		t.Fatal(err)
	}
	if (want == 0 && (alias != "" || gate != "closed")) || (want == 1 && (alias != "driver-free publication" || gate != "open")) {
		t.Fatalf("owner/barrier atomicity: %q %q", alias, gate)
	}
}
