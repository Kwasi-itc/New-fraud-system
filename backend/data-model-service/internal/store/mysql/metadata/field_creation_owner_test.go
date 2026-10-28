package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func verifyPreparedFieldOwner(t *testing.T, ctx context.Context, db *sql.DB, database string, tenantID uuid.UUID, field datamodel.Field, r store.PhysicalPublicationRequest, name string, policies []store.PhysicalAccessPolicy, shared *installation.ExactKeyReadiness, installer installation.RecordInstallerPolicy, scenario string) {
	t.Helper()
	resultMode := strings.Contains(scenario, "_result_")
	var originalResult datamodel.Field
	var resultMu sync.Mutex
	concurrent := strings.Contains(scenario, "_concurrent_")
	if concurrent {
		db.SetMaxOpenConns(8)
		defer db.SetMaxOpenConns(1)
	}
	start := make(chan struct{})
	schema, _ := mysqlstore.Identifier(database)
	if strings.HasSuffix(scenario, "_drift") {
		if _, err := db.ExecContext(ctx, "UPDATE "+schema+".data_model_fields SET alias='drift' WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(field.ID)); err != nil {
			t.Fatal(err)
		}
	}
	if strings.HasSuffix(scenario, "_inactive") {
		if _, err := db.ExecContext(ctx, "UPDATE "+schema+".data_model_tenants SET status='pending' WHERE id=?", mysqlstore.UUID(tenantID)); err != nil {
			t.Fatal(err)
		}
	}
	denied := false
	var preCalls atomic.Int32
	localCalls := 0
	receiptLimited := strings.HasSuffix(scenario, "_receipt_limit")
	makeManager := func(pool *sql.DB) *store.PhysicalPublicationManager {
		limit := 1 << 20
		if receiptLimited {
			limit = 256
		}
		metadata, err := store.NewTransactionManager(pool, database, tenantID, 100, limit, func(context.Context, *sql.Tx, uuid.UUID) error { localCalls++; return nil }, nil)
		if err != nil {
			t.Fatal(err)
		}
		publication, err := store.NewPublicationStore(pool, database, 100, 100, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		manager, err := store.NewPhysicalPublicationManager(metadata, publication, store.PhysicalTable{TableID: field.TableID, Name: name}, policies, func(ctx context.Context, tenant uuid.UUID, request ports.PhysicalPublicationRequest) error {
			call := preCalls.Add(1)
			if concurrent && call <= 8 {
				if call == 8 {
					close(start)
				}
				select {
				case <-start:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if denied || tenant != tenantID || request.ActorID != r.ActorID || request.TableID != field.TableID {
				return errors.New("denied")
			}
			return db.PingContext(ctx)
		})
		if err != nil {
			t.Fatal(err)
		}
		manager, err = manager.WithCreationReadiness(shared, installer)
		if err != nil {
			t.Fatal(err)
		}
		return manager
	}
	manager := makeManager(db)
	ids := &creationOwnerIDs{}
	if strings.HasSuffix(scenario, "_audit_failure") {
		ids.fail = 1
	}
	if strings.HasSuffix(scenario, "_revision_failure") {
		ids.fail = 2
	}
	owner := service.NewFieldService(nil, nil, nil, nil, nil, nil, nil, nil, nil, ids, creationOwnerClock{})
	publish := func(ctx context.Context, tenant, id uuid.UUID, in service.CreateFieldInput, request ports.PhysicalPublicationRequest, m ports.PhysicalPublicationManager) (contract.Publication, error) {
		if !resultMode {
			return owner.PublishPreparedCreation(ctx, tenant, id, in, request, m)
		}
		typed := m.(ports.PhysicalFieldPublicationManager)
		saved, e := owner.PublishPreparedCreationResult(ctx, tenant, id, in, request, typed)
		if e != nil {
			if saved.ID != uuid.Nil {
				t.Fatal("provisional Field escaped")
			}
			return contract.Publication{}, e
		}
		resultMu.Lock()
		defer resultMu.Unlock()
		if originalResult.ID != uuid.Nil && saved != originalResult {
			t.Fatal("original Field changed")
		}
		originalResult = saved
		return recoveredPhysicalResultPublication(t, ctx, db, database, tenant, request, saved, id)
	}
	input := service.CreateFieldInput{TableID: field.TableID, Name: field.Name, DataType: field.DataType, Nullable: field.Nullable}
	request := ports.PhysicalPublicationRequest{TableID: field.TableID, OperationID: r.Operation.ID, RequestHash: r.Operation.RequestHash, Condition: r.Operation.Condition, ActorKind: r.ActorKind, ActorID: r.ActorID}
	selected := manager
	var observed atomic.Bool
	if strings.HasSuffix(scenario, "_lost_commit") {
		selected = makeManager(deliveryFaultDB(t, database, &observed))
	}
	var result contract.Publication
	var err error
	if concurrent {
		var wg sync.WaitGroup
		results := make(chan contract.Publication, 8)
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p, e := publish(ctx, tenantID, field.ID, input, request, selected)
				results <- p
				errs <- e
			}()
		}
		wg.Wait()
		close(results)
		close(errs)
		busy := 0
		for e := range errs {
			if errors.Is(e, mysqlstore.ErrDDLLockBusy) {
				busy++
			} else if e != nil {
				t.Fatal(e)
			}
		}
		successes := 0
		for p := range results {
			if p.Version == uuid.Nil {
				continue
			}
			successes++
			if result.Version != uuid.Nil && result.Version != p.Version {
				t.Fatal("concurrent publication versions differ")
			}
			result = p
		}
		if successes == 0 || successes+busy != 8 {
			t.Fatal("concurrent busy/success outcomes", successes, busy)
		}
		for i := 0; i < 8; i++ {
			p, e := publish(ctx, tenantID, field.ID, input, request, manager)
			if e != nil || p.Version != result.Version {
				t.Fatal("busy operation recovery", e)
			}
		}
	} else {
		result, err = publish(ctx, tenantID, field.ID, input, request, selected)
	}
	if strings.HasSuffix(scenario, "_lost_commit") {
		var ambiguous *mysqlstore.CommitError
		if !errors.As(err, &ambiguous) || !observed.Load() || result.Version != uuid.Nil {
			t.Fatal("field ambiguous commit", err)
		}
		sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + field.TableID.String()))
		lock := fmt.Sprintf("data_model_ddl_%x", sum[:24])
		conn, e := db.Conn(ctx)
		if e != nil {
			t.Fatal(e)
		}
		var acquired int
		if e := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,5)", lock).Scan(&acquired); e != nil || acquired != 1 {
			t.Fatal("old field owner lock", e)
		}
		if e := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lock).Scan(&acquired); e != nil || acquired != 1 {
			t.Fatal(e)
		}
		if e := conn.Close(); e != nil {
			t.Fatal(e)
		}
		result, err = publish(ctx, tenantID, field.ID, input, request, manager)
	}
	if receiptLimited && (!errors.Is(err, store.ErrSnapshotLimit) || localCalls != 1 || ids.calls != 2) {
		t.Fatal("receipt failure must follow owner effects", err, localCalls, ids.calls)
	}
	want := 0
	if strings.HasSuffix(scenario, "_commit") {
		want = 1
		if err != nil || result.Version == uuid.Nil || ids.calls != 2 || localCalls != 1 {
			t.Fatal("field owner publication", err, ids.calls, localCalls)
		}
		before := preCalls.Load()
		replay, e := publish(ctx, tenantID, field.ID, input, request, manager)
		if e != nil || replay.Version != result.Version || ids.calls != 2 || localCalls != 1 || preCalls.Load() != before+1 {
			t.Fatal("field replay", e)
		}
		denied = true
		if _, e := publish(ctx, tenantID, field.ID, input, request, manager); e == nil {
			t.Fatal("revoked field replay")
		}
		denied = false
		if _, e := publish(ctx, tenantID, uuid.New(), input, request, manager); e == nil {
			t.Fatal("substituted field identity")
		}
		input.Alias = "changed command"
		if _, e := publish(ctx, tenantID, field.ID, input, request, manager); e == nil {
			t.Fatal("changed field command")
		}
	} else if err == nil || result.Version != uuid.Nil {
		t.Fatal("unsafe field publication", err)
	}
	var audits, revisions int
	if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_schema_change_log WHERE tenant_id=? AND resource_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(field.ID)).Scan(&audits); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_tenant_schema_migrations WHERE tenant_id=?", mysqlstore.UUID(tenantID)).Scan(&revisions); e != nil {
		t.Fatal(e)
	}
	if audits != want || revisions != 1+want {
		t.Fatal("field owner atomic effects", audits, revisions, want)
	}
	var gate string
	if e := db.QueryRowContext(ctx, "SELECT write_state FROM "+schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(field.TableID)).Scan(&gate); e != nil {
		t.Fatal(e)
	}
	if (want == 0 && gate != "closed") || (want == 1 && gate != "open") {
		t.Fatal("field owner gate", gate)
	}
	if resultMode {
		verifyPhysicalFieldReceipt(t, ctx, db, database, tenantID, field.TableID, request, manager, originalResult, want)
	}
	if receiptLimited {
		receiptLimited = false
		healthy := makeManager(db)
		if _, e := publish(ctx, tenantID, field.ID, input, request, healthy); e != nil {
			t.Fatal("receipt-only recovery", e)
		}
		if localCalls != 2 || ids.calls != 4 {
			t.Fatal("receipt recovery effects", localCalls, ids.calls)
		}
		verifyPhysicalFieldReceipt(t, ctx, db, database, tenantID, field.TableID, request, healthy, originalResult, 1)
	}
	if !resultMode && want == 1 {
		if _, e := manager.PublishField(ctx, request, field.ID, func(ports.MutationStore) error { t.Fatal("legacy replay mutated"); return nil }); !errors.Is(e, store.ErrOwnerResultUnavailable) {
			t.Fatal("legacy result reconstructed", e)
		}
	}
}
