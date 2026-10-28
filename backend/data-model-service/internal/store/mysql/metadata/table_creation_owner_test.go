package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type creationOwnerIDs struct{ calls, fail int }

func (g *creationOwnerIDs) New() uuid.UUID {
	g.calls++
	if g.calls == g.fail {
		return uuid.Nil
	}
	return uuid.New()
}

type creationOwnerClock struct{}

func (creationOwnerClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func verifyPreparedCreationOwner(t *testing.T, ctx context.Context, db *sql.DB, database string, tenantID uuid.UUID, physical store.PhysicalTable, r store.PhysicalPublicationRequest, policies []store.PhysicalAccessPolicy, shared *installation.ExactKeyReadiness, installer installation.RecordInstallerPolicy, scenario string) {
	t.Helper()
	schema, _ := mysqlstore.Identifier(database)
	if scenario != "service_inactive" {
		if _, err := db.ExecContext(ctx, "UPDATE "+schema+".data_model_tenants SET status='active' WHERE id=?", mysqlstore.UUID(tenantID)); err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "service_drift" {
		if _, err := db.ExecContext(ctx, "UPDATE "+schema+".data_model_tables SET alias='unexpected' WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(physical.TableID)); err != nil {
			t.Fatal(err)
		}
	}
	localCalls, preCalls := 0, 0
	metadata, err := store.NewTransactionManager(db, database, tenantID, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID) error { localCalls++; return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := store.NewPublicationStore(db, database, 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	denied := false
	preAuthorize := func(ctx context.Context, scope uuid.UUID, request ports.PhysicalPublicationRequest) error {
		preCalls++
		if denied || scope != tenantID || request.ActorID != r.ActorID || request.TableID != physical.TableID {
			return errors.New("denied")
		}
		// One-slot pool: authorization must precede the pinned execution lock.
		return db.PingContext(ctx)
	}
	manager, err := store.NewPhysicalPublicationManager(metadata, publication, physical, policies, preAuthorize)
	if err != nil {
		t.Fatal(err)
	}
	manager, err = manager.WithCreationReadiness(shared, installer)
	if err != nil {
		t.Fatal(err)
	}
	ids := &creationOwnerIDs{}
	if scenario == "service_audit_failure" {
		ids.fail = 1
	}
	if scenario == "service_revision_failure" {
		ids.fail = 2
	}
	owner := service.NewTableService(nil, nil, nil, nil, nil, nil, nil, nil, ids, creationOwnerClock{})
	request := ports.PhysicalPublicationRequest{TableID: physical.TableID, OperationID: r.Operation.ID, RequestHash: r.Operation.RequestHash, Condition: r.Operation.Condition, ActorKind: r.ActorKind, ActorID: r.ActorID}
	input := service.CreateTableInput{TenantID: tenantID, Name: "records"}
	selected := manager
	var observed atomic.Bool
	if scenario == "service_allocated_lost_commit" {
		faultDB := deliveryFaultDB(t, database, &observed)
		faultMetadata, e := store.NewTransactionManager(faultDB, database, tenantID, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID) error { localCalls++; return nil }, nil)
		if e != nil {
			t.Fatal(e)
		}
		faultPublication, e := store.NewPublicationStore(faultDB, database, 100, 100, 1<<20)
		if e != nil {
			t.Fatal(e)
		}
		fault, e := store.NewPhysicalPublicationManager(faultMetadata, faultPublication, physical, policies, preAuthorize)
		if e != nil {
			t.Fatal(e)
		}
		selected, e = fault.WithCreationReadiness(shared, installer)
		if e != nil {
			t.Fatal(e)
		}
	}
	result, publishErr := owner.PublishPreparedCreation(ctx, input, request, selected)
	wantPre := 2
	if scenario == "service_allocated_lost_commit" {
		var uncertain *mysqlstore.CommitError
		if !errors.As(publishErr, &uncertain) || !observed.Load() || result.Version != uuid.Nil {
			t.Fatal("owner ambiguous publication", publishErr)
		}
		sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + physical.TableID.String()))
		lock := fmt.Sprintf("data_model_ddl_%x", sum[:24])
		conn, e := db.Conn(ctx)
		if e != nil {
			t.Fatal(e)
		}
		var acquired int
		if e := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,5)", lock).Scan(&acquired); e != nil || acquired != 1 {
			t.Fatal("old owner session remains", e)
		}
		if e := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lock).Scan(&acquired); e != nil || acquired != 1 {
			t.Fatal(e)
		}
		if e := conn.Close(); e != nil {
			t.Fatal(e)
		}
		result, publishErr = owner.PublishPreparedCreation(ctx, input, request, manager)
		wantPre = 3
	}
	want := 0
	if scenario == "service_commit" || strings.HasPrefix(scenario, "service_allocated") {
		want = 1
		if publishErr != nil || result.Version == uuid.Nil || ids.calls != 2 || localCalls != 1 {
			t.Fatal("owner publication", publishErr, ids.calls, localCalls)
		}
		replay, err := owner.PublishPreparedCreation(ctx, input, request, manager)
		if err != nil || replay.Version != result.Version || ids.calls != 2 || localCalls != 1 || preCalls != wantPre {
			t.Fatal("owner replay", err, ids.calls, localCalls, preCalls)
		}
		denied = true
		if _, err := owner.PublishPreparedCreation(ctx, input, request, manager); err == nil {
			t.Fatal("unauthorized replay accepted")
		}
		denied = false
		changed := input
		changed.Alias = "changed"
		if _, err := owner.PublishPreparedCreation(ctx, changed, request, manager); err == nil {
			t.Fatal("changed original owner input accepted")
		}
	} else if publishErr == nil || result.Version != uuid.Nil {
		t.Fatal("invalid owner creation succeeded", publishErr)
	}
	for _, name := range []string{"data_model_schema_change_log", "data_model_tenant_schema_migrations", "data_model_publication_operations"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+name+" WHERE tenant_id=?", mysqlstore.UUID(tenantID)).Scan(&count); err != nil || count != want {
			t.Fatal("owner atomic effects", name, count, want, err)
		}
	}
	var gate string
	if err := db.QueryRowContext(ctx, "SELECT write_state FROM "+schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(physical.TableID)).Scan(&gate); err != nil {
		t.Fatal(err)
	}
	if (want == 1 && gate != "open") || (want == 0 && gate != "closed") {
		t.Fatal("owner gate", gate)
	}
}
