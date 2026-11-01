package mysqlexecution

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLActualEvaluationReadersScopeAbsenceAndRevocation(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	if _, err := f.reads.RiskSnapshots().GetByObject(ctx, f.tenant.String(), "wallets", "missing"); !errors.Is(err, ports.ErrRiskSnapshotNotFound) {
		t.Fatal("absence contract lost", err)
	}
	for _, q := range []string{
		"INSERT INTO " + f.schema + ".decision_engine_record_tags(id,tenant_id,object_type,object_id,tag,created_at) VALUES(?,?,'wallets',' exact ','watch',UTC_TIMESTAMP(6))",
		"INSERT INTO " + f.schema + ".decision_engine_risk_snapshots(id,tenant_id,object_type,object_id,risk_level,created_at) VALUES(?,?,'wallets',' exact ','high',UTC_TIMESTAMP(6))",
		"INSERT INTO " + f.schema + ".decision_engine_ip_flags(id,tenant_id,ip_address,flag,created_at) VALUES(?,?,'192.0.2.10','watch',UTC_TIMESTAMP(6))",
		"INSERT INTO " + f.schema + ".decision_engine_custom_list_entries(id,tenant_id,list_name,value,created_at) VALUES(?,?,'watch',' exact ',UTC_TIMESTAMP(6))",
	} {
		if _, err := f.db.Exec(q, mysqlstore.UUID(uuid.New()), mysqlstore.UUID(f.tenant)); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := f.reads.RecordTags().HasTag(ctx, f.tenant.String(), "wallets", " exact ", "watch"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if risk, err := f.reads.RiskSnapshots().GetByObject(ctx, f.tenant.String(), "wallets", " exact "); err != nil || risk == nil || risk.RiskLevel != "high" {
		t.Fatal(risk, err)
	}
	if ok, err := f.reads.IPFlags().HasFlag(ctx, f.tenant.String(), "192.0.2.10", "watch"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := f.reads.CustomLists().Contains(ctx, f.tenant.String(), "watch", " exact "); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := f.reads.RecordTags().HasTag(ctx, uuid.NewString(), "wallets", " exact ", "watch"); !errors.Is(err, mysql.ErrTenantScope) || ok {
		t.Fatal("foreign reader", ok, err)
	}
	if _, err := f.reads.Snoozes().ListActive(ctx, uuid.NewString(), f.parent.String(), "wallets", " exact ", clock{}.Now()); !errors.Is(err, mysql.ErrTenantScope) {
		t.Fatal(err)
	}
	f.revoked.Store(true)
	if _, err := f.reads.RecordTags().HasTag(ctx, f.tenant.String(), "wallets", " exact ", "watch"); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) {
		t.Fatal("revoked read", err)
	}
}
func TestMySQLTargetPreparerRequiresRealSameScopeReadersAndExplicitPolicy(t *testing.T) {
	f := setupPreparation(t)
	policy := EvaluationPolicy{Limits: service.ExecutionPreparationLimits{MaxItems: 10, MaxBytes: 2 << 20}, Aggregates: []string{"count"}}
	if _, err := NewTargetPreparer(f.executions, f.preparer.reader, mysql.EvaluationReaders{}, ids{}, clock{}, nil, policy); err == nil {
		t.Fatal("missing readers accepted")
	}
	for _, aggregates := range [][]string{nil, {"count", "count"}, {"count", "unsupported"}} {
		policy.Aggregates = aggregates
		if _, err := NewTargetPreparer(f.executions, f.preparer.reader, f.reads, ids{}, clock{}, nil, policy); err == nil {
			t.Fatal("invalid aggregate policy", aggregates)
		}
	}
	foreign, err := mysql.NewExecutionStore(f.db, f.database, uuid.New(), 500, 2<<20, func(context.Context, *sql.Tx, uuid.UUID, mysql.ExecutionAction) error { return nil }, mysql.ExecutionPolicy{Queues: map[ports.JobKind]mysql.DeliveryQueuePolicy{ports.JobScheduled: {Name: string(ports.JobScheduled), MaxAttempts: 3}, ports.JobAsync: {Name: string(ports.JobAsync), MaxAttempts: 3}, ports.JobCallback: {Name: string(ports.JobCallback), MaxAttempts: 3}}, CallbackMaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	if f.reads.MatchesExecutionStore(foreign) {
		t.Fatal("foreign read set accepted")
	}
}
