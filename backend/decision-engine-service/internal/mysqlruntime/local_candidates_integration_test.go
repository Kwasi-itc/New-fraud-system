package mysqlruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/localtenants"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestLocalTenantWorkDiscoveryUsesIndexedDueQueues(t *testing.T) {
	db, name, _, _ := testsupport.DecisionMySQLRuntimeDatabase(t)
	schema, _ := mysqlstore.Identifier(name)
	ctx := context.Background()
	// Declared registry consumer contract; queue tables/triggers use all actual
	// decision-owner migrations installed by the runtime fixture above.
	if _, err := db.Exec("CREATE TABLE " + schema + ".data_model_tenants(id BINARY(16) PRIMARY KEY,status VARCHAR(16) NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	for range 123 {
		if _, err := db.Exec("INSERT INTO "+schema+".data_model_tenants VALUES(?,'active')", mysqlstore.UUID(uuid.New())); err != nil {
			t.Fatal(err)
		}
	}
	runtime := &Runtime{deps: Dependencies{DB: db, Database: name}, policy: localPolicy()}
	for _, kind := range allKinds {
		for _, cleanup := range []bool{false, true} {
			ready, err := runtime.localMaintenanceReady(ctx, uuid.New(), kind, cleanup)
			if err != nil || ready {
				t.Fatal("idle maintenance", kind, cleanup, ready, err)
			}
		}
		page, err := runtime.localClaimCandidates(ctx, kind, uuid.Nil)
		if err != nil || len(page) != 20 {
			t.Fatal(kind, len(page), err)
		}
		for _, row := range page {
			if row.Ready {
				t.Fatal("idle tenant reported due", kind, row)
			}
		}
	}
	page, err := runtime.localClaimCandidates(ctx, ports.JobOutbox, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	tenant := page[0].ID
	registry, err := localtenants.New(db, name)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(ctx context.Context, tx *sql.Tx, id uuid.UUID, _ bool) error {
		if id != tenant {
			return store.ErrDecisionAuthorizationDenied
		}
		err := registry.Authorize(ctx, tx, id)
		if err == localtenants.ErrDenied {
			return store.ErrDecisionAuthorizationDenied
		}
		return err
	}
	owner, err := store.NewQueuedEffectStore(db, name, tenant, 500, 4<<20, authorize, nil, runtime.policy.Effects)
	if err != nil {
		t.Fatal(err)
	}
	event := integration.OutboxEvent{ID: uuid.NewString(), TenantID: tenant.String(), AggregateType: "tenant", AggregateID: tenant.String(), EventType: "synthetic.discovery", Payload: json.RawMessage(`{}`), Status: integration.OutboxStatusPending, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	second := event
	second.ID = uuid.NewString()
	if err := owner.Mutate(ctx, func(r store.EffectRepositories) error {
		_, err := r.OutboxEvents.CreateMany(ctx, []integration.OutboxEvent{event, second})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	page, err = runtime.localClaimCandidates(ctx, ports.JobOutbox, uuid.Nil)
	if err != nil || !page[0].Ready || !page[0].Active {
		t.Fatal(page, err)
	}
	query, args, err := runtime.localCandidateQuery(ports.JobOutbox, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	var plan string
	if err := db.QueryRowContext(ctx, "EXPLAIN ANALYZE "+query, args...).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	t.Log(plan)
	if !strings.Contains(plan, "PRIMARY") || !strings.Contains(plan, "de_effect_delivery_claim") || strings.Contains(strings.ToLower(plan), "table scan") {
		t.Fatal("queue discovery scans history", plan)
	}
	// Claim-only consumer model/dispatcher fixtures cannot report successful
	// external work. All queue claims and owner authority below use real SQL.
	runtime.deps.Models = &fixtureModel{tenant: tenant, model: ports.TenantModel{RevisionID: "fixture-model", Tables: map[string]ports.TenantModelTable{}}}
	runtime.deps.IDs = fixtureIDs{}
	runtime.deps.Clock = fixtureClock{}
	runtime.deps.Dispatch = map[ports.JobKind]ports.ReceiptDispatcher{ports.JobOutbox: unusedDispatch{}}
	scopes := 0
	runtime.deps.ServiceAuthority = func(_ context.Context, id uuid.UUID) (Authorities, error) {
		scopes++
		if id != tenant {
			return Authorities{}, store.ErrDecisionAuthorizationDenied
		}
		return Authorities{TenantID: id, Authoring: func(ctx context.Context, tx *sql.Tx, t uuid.UUID, _ store.AuthoringAction) error {
			return authorize(ctx, tx, t, false)
		}, Reads: authorize, Persist: authorize, Executions: func(ctx context.Context, tx *sql.Tx, t uuid.UUID, _ store.ExecutionAction) error {
			return authorize(ctx, tx, t, false)
		}, Worker: func(ctx context.Context, tx *sql.Tx, t uuid.UUID, _ ports.JobKind, _ store.EffectWorkerAction) error {
			return authorize(ctx, tx, t, false)
		}}, nil
	}
	var cursor uuid.UUID
	for range 2 {
		task, err := runtime.localClaimTask(ctx, ports.JobOutbox, &cursor)
		if err != nil || task == nil {
			t.Fatal("idle pages delayed ready tenant backlog", task, err)
		}
	}
	if scopes != 2 {
		t.Fatal("constructed owner graphs for idle tenants", scopes)
	}
	var running int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+schema+".decision_engine_effect_job_deliveries WHERE tenant_id=? AND kind=? AND state='running'", mysqlstore.UUID(tenant), string(ports.JobOutbox)).Scan(&running); err != nil || running != 2 {
		t.Fatal(running, err)
	}
	maintenance := &localMaintenance{runtime, registry, ports.JobOutbox}
	if n, _, err := maintenance.MaintainPage(ctx, 20, false, &localMaintenanceCursor{Tenant: page[1].ID}); err != nil || n != 0 || scopes != 2 {
		t.Fatal("idle maintenance constructed an owner", n, scopes, err)
	}
	if _, err := db.Exec("UPDATE "+schema+".decision_engine_effect_job_deliveries SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE tenant_id=?", mysqlstore.UUID(tenant)); err != nil {
		t.Fatal(err)
	}
	for _, cleanup := range []bool{false, true} {
		query, args, err := runtime.localMaintenanceQuery(tenant, ports.JobOutbox, cleanup)
		if err != nil {
			t.Fatal(err)
		}
		var explain string
		if err := db.QueryRowContext(ctx, "EXPLAIN ANALYZE "+query, args...).Scan(&explain); err != nil {
			t.Fatal(err)
		}
		t.Log(explain)
		if strings.Contains(strings.ToLower(explain), "table scan") {
			t.Fatal("maintenance scanned history", explain)
		}
	}
	if n, _, err := maintenance.MaintainPage(ctx, 20, false, &localMaintenanceCursor{Tenant: tenant}); err != nil || n != 2 {
		t.Fatal("actual expired deliveries were not rescued", n, err)
	}
	if _, err := db.Exec("UPDATE "+schema+".decision_engine_effect_job_deliveries SET state='completed',finished_at=UTC_TIMESTAMP(6)-INTERVAL 2 DAY WHERE tenant_id=?", mysqlstore.UUID(tenant)); err != nil {
		t.Fatal(err)
	}
	if n, _, err := maintenance.MaintainPage(ctx, 20, true, &localMaintenanceCursor{Tenant: tenant}); err != nil || n != 2 {
		t.Fatal("actual aged deliveries were not cleaned", n, err)
	}
	var retained int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+schema+".decision_engine_effect_job_receipts WHERE tenant_id=?", mysqlstore.UUID(tenant)).Scan(&retained); err != nil || retained != 2 {
		t.Fatal("permanent receipts removed", retained, err)
	}
	third := event
	third.ID = uuid.NewString()
	if err := owner.Mutate(ctx, func(r store.EffectRepositories) error {
		_, err := r.OutboxEvents.CreateMany(ctx, []integration.OutboxEvent{third})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE "+schema+".decision_engine_effect_job_deliveries SET state='completed',finished_at=UTC_TIMESTAMP(6) WHERE tenant_id=?", mysqlstore.UUID(tenant)); err != nil {
		t.Fatal(err)
	}
	if ready, err := runtime.localMaintenanceReady(ctx, tenant, ports.JobOutbox, true); err != nil || ready {
		t.Fatal("recent completion eligible for cleanup", ready, err)
	}
	if _, err := db.Exec("UPDATE "+schema+".decision_engine_effect_job_deliveries SET state='discarded',finished_at=UTC_TIMESTAMP(6)-INTERVAL 2 DAY WHERE tenant_id=?", mysqlstore.UUID(tenant)); err != nil {
		t.Fatal(err)
	}
	if ready, err := runtime.localMaintenanceReady(ctx, tenant, ports.JobOutbox, true); err != nil || ready {
		t.Fatal("discard retention shortened", ready, err)
	}
	if _, err := db.Exec("UPDATE "+schema+".decision_engine_effect_job_deliveries SET finished_at=UTC_TIMESTAMP(6)-INTERVAL 8 DAY WHERE tenant_id=?", mysqlstore.UUID(tenant)); err != nil {
		t.Fatal(err)
	}
	if ready, err := runtime.localMaintenanceReady(ctx, tenant, ports.JobOutbox, true); err != nil || !ready {
		t.Fatal("aged discard not discovered", ready, err)
	}
	if _, err := db.Exec("UPDATE "+schema+".data_model_tenants SET status='paused' WHERE id=?", mysqlstore.UUID(tenant)); err != nil {
		t.Fatal(err)
	}
	// Discovery is never authority: revocation after a positive readiness check
	// must prevent the real owner's deletion and retain the delivery.
	if n, _, err := maintenance.MaintainPage(ctx, 20, true, &localMaintenanceCursor{Tenant: tenant}); err != nil || n != 0 {
		t.Fatal("paused tenant cleanup", n, err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM "+schema+".decision_engine_effect_job_deliveries WHERE tenant_id=?", mysqlstore.UUID(tenant)).Scan(&retained); err != nil || retained != 1 {
		t.Fatal("revoked cleanup removed delivery", retained, err)
	}
	page, err = runtime.localClaimCandidates(ctx, ports.JobOutbox, uuid.Nil)
	if err != nil || page[0].Active {
		t.Fatal("activation cached", page, err)
	}
}
