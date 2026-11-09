package mysql

import (
	"context"
	"errors"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlschema"
	"github.com/google/uuid"
)

// Both schemas install actual decision-owner migrations. Only the generated
// reference schema is observed to bind the screening deployment artifact.
func installScreeningOwner(t *testing.T, f effectFixture) {
	t.Helper()
	ref := effectsFixture(t)
	ctx := context.Background()
	steps, err := mysqlschema.Statements(ref.database, "root")
	if err != nil {
		t.Fatal(err)
	}
	target, err := mysqlschema.Statements(f.database, "root")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := ref.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	prior := map[string]string{}
	for i, s := range steps {
		target[i].Before = prior[s.Kind+":"+s.Name]
		if _, err := conn.ExecContext(ctx, s.SQL); err != nil {
			t.Fatalf("screening reference %s: %v", s.ID, err)
		}
		definition, err := migrations.Definition(ctx, conn, ref.database, s.Kind, s.Name)
		if err != nil {
			t.Fatal(err)
		}
		target[i].After = strings.ReplaceAll(definition, ref.database, f.database)
		prior[s.Kind+":"+s.Name] = target[i].After
	}
	plan, err := mysqlschema.Plan(f.database, "root", target)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := migrations.NewForOwner(f.db, "screening", plan)
	if err != nil {
		t.Fatal(err)
	}
	if err = runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if err = runner.Apply(ctx); err != nil {
		t.Fatal("owner migration replay", err)
	}
	if err = runner.Verify(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLScreeningOwnerPublicationBindingsAndView(t *testing.T) {
	f := effectsFixture(t)
	installScreeningOwner(t, f)
	ctx := context.Background()
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	execution := f.screeningInput(d)
	if err := f.store.Mutate(ctx, func(r EffectRepositories) error {
		if _, err := r.Decisions.Create(ctx, d); err != nil {
			return err
		}
		_, err := r.ScreeningExecutions.CreateMany(ctx, []screening.Execution{execution})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	insert := func(id, tenant, decision, scenario, config, exec uuid.UUID, version int64) error {
		_, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+`.screening_screenings
   (id,tenant_id,decision_id,scenario_id,screening_config_id,execution_id,projection_version,status,provider,object_type,object_id,request_json,response_json,decision_projection,provider_reference,last_error,created_at,updated_at)
   VALUES(?,?,?,?,?,?,?,'queued','provider','account','1',JSON_OBJECT(),JSON_OBJECT('private','secret'),JSON_OBJECT('match_count',0),'','',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, mysqlstore.UUID(id), mysqlstore.UUID(tenant), mysqlstore.UUID(decision), mysqlstore.UUID(scenario), mysqlstore.UUID(config), mysqlstore.UUID(exec), version)
		return err
	}
	did, cid, eid := uuid.MustParse(d.ID), uuid.MustParse(f.screen.ID), uuid.MustParse(execution.ID)
	other := decisionInput(f.tenant, f.parent, f.iteration.ID)
	if err := f.store.Mutate(ctx, func(r EffectRepositories) error { _, err := r.Decisions.Create(ctx, other); return err }); err != nil {
		t.Fatal(err)
	}
	t.Run("existing_but_mismatched_decision", func(t *testing.T) {
		if err := insert(uuid.New(), f.tenant, uuid.MustParse(other.ID), f.parent, cid, eid, 1); err == nil {
			t.Fatal("unrelated existing decision accepted")
		}
	})
	for _, test := range []struct {
		name                                          string
		tenant, decision, scenario, config, execution uuid.UUID
		version                                       int64
	}{
		{"foreign_tenant", uuid.New(), did, f.parent, cid, eid, 1},
		{"unknown_decision", f.tenant, uuid.New(), f.parent, cid, eid, 1},
		{"wrong_scenario", f.tenant, did, uuid.New(), cid, eid, 1},
		{"wrong_config", f.tenant, did, f.parent, uuid.New(), eid, 1},
		{"unknown_execution", f.tenant, did, f.parent, cid, uuid.New(), 1},
		{"zero_version", f.tenant, did, f.parent, cid, eid, 0},
		{"negative_version", f.tenant, did, f.parent, cid, eid, -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := insert(uuid.New(), test.tenant, test.decision, test.scenario, test.config, test.execution, test.version); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
	source := uuid.New()
	if err := insert(source, f.tenant, did, f.parent, cid, eid, 1); err != nil {
		t.Fatal(err)
	}
	t.Run("duplicate_execution", func(t *testing.T) {
		if err := insert(uuid.New(), f.tenant, did, f.parent, cid, eid, 1); err == nil {
			t.Fatal("duplicate binding accepted")
		}
	})
	for _, assignment := range []string{"tenant_id=RANDOM_BYTES(16)", "execution_id=NULL", "decision_id=RANDOM_BYTES(16)", "screening_config_id=RANDOM_BYTES(16)"} {
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".screening_screenings SET projection_version=2,"+assignment+" WHERE id=?", mysqlstore.UUID(source)); err == nil {
			t.Fatal("mutable binding", assignment)
		}
	}
	var updatable string
	if err := f.db.QueryRowContext(ctx, "SELECT IS_UPDATABLE FROM information_schema.VIEWS WHERE TABLE_SCHEMA=? AND TABLE_NAME='screening_decision_source_v1'", f.database).Scan(&updatable); err != nil || updatable != "YES" {
		t.Fatal("view not updatable", updatable, err)
	}
	contract, err := NewScreeningSourceContract(f.database, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("manual_rows_not_published", func(t *testing.T) {
		id := uuid.New()
		if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+`.screening_screenings
		(id,tenant_id,status,provider,object_type,object_id,request_json,response_json,decision_projection,provider_reference,last_error,created_at,updated_at)
		VALUES(?,?,'pending','manual','account','2',JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT(),'','',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, mysqlstore.UUID(id), mysqlstore.UUID(f.tenant)); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"screening_decision_source_v1", "screening_execution_projection_queue"} {
			column := "id"
			if table == "screening_execution_projection_queue" {
				column = "screening_id"
			}
			var count int
			if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+table+" WHERE tenant_id=? AND "+column+"=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(id)).Scan(&count); err != nil || count != 0 {
				t.Fatal("manual row published", table, count, err)
			}
		}
	})
	for _, test := range []struct {
		name                     string
		tenant, decision, config uuid.UUID
	}{
		{"foreign_reader", uuid.New(), did, cid}, {"wrong_expected_decision", f.tenant, uuid.MustParse(other.ID), cid}, {"wrong_expected_config", f.tenant, did, uuid.New()},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := f.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := contract.Lock(ctx, tx, test.tenant, source, test.decision, test.config); err == nil {
				t.Fatal("wrong source scope accepted")
			}
		})
	}
	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	publication, err := contract.Lock(ctx, tx, f.tenant, source, did, cid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(publication.Response), "secret") || publication.Version != 1 || !publication.ExecutionID.Valid {
		t.Fatal("incorrect source projection", publication)
	}
	t.Run("view_lock_reaches_owner_row", func(t *testing.T) {
		conn, err := f.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err = conn.ExecContext(ctx, "SET SESSION innodb_lock_wait_timeout=1"); err != nil {
			t.Fatal(err)
		}
		defer conn.ExecContext(ctx, "SET SESSION innodb_lock_wait_timeout=50")
		waiting, err := conn.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer waiting.Rollback()
		_, err = waiting.ExecContext(ctx, "UPDATE "+schema+".screening_screenings SET status='processing',projection_version=2 WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(source))
		var serverError *mysqldriver.MySQLError
		if !errors.As(err, &serverError) || serverError.Number != 1205 {
			t.Fatal("source update bypassed view row lock", err)
		}
	})
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".screening_screenings SET status='processing',projection_version=2 WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(source)); err != nil {
		t.Fatal("source remains writable after release", err)
	}
	t.Run("stale_version_rejected", func(t *testing.T) {
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".screening_screenings SET status='failed' WHERE id=?", mysqlstore.UUID(source)); err == nil {
			t.Fatal("unversioned mutation accepted")
		}
	})
	t.Run("bounded_projection", func(t *testing.T) {
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".screening_screenings SET projection_version=3,provider_reference=REPEAT('x',16385) WHERE id=?", mysqlstore.UUID(source)); err == nil {
			t.Fatal("oversized publication accepted")
		}
	})
	t.Run("source_and_enqueue_rollback_together", func(t *testing.T) {
		if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".reject_projection BEFORE UPDATE ON "+schema+".screening_execution_projection_queue FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected enqueue failure'"); err != nil {
			t.Fatal(err)
		}
		_, err := f.db.ExecContext(ctx, "UPDATE "+schema+".screening_screenings SET projection_version=3,status='failed' WHERE id=?", mysqlstore.UUID(source))
		if err == nil {
			t.Fatal("source committed despite enqueue failure")
		}
		if _, err = f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".reject_projection"); err != nil {
			t.Fatal(err)
		}
		var sourceVersion, queueVersion int64
		if err = f.db.QueryRowContext(ctx, "SELECT s.projection_version,q.version FROM "+schema+".screening_screenings s JOIN "+schema+".screening_execution_projection_queue q ON s.tenant_id=q.tenant_id AND s.id=q.screening_id WHERE s.id=?", mysqlstore.UUID(source)).Scan(&sourceVersion, &queueVersion); err != nil || sourceVersion != 2 || queueVersion != 2 {
			t.Fatal("publication rollback", sourceVersion, queueVersion, err)
		}
	})
	t.Run("real_source_reconciliation", func(t *testing.T) {
		for i := 0; i < 2; i++ {
			if err := f.store.Mutate(ctx, func(r EffectRepositories) error {
				result, err := r.ScreeningExecutions.ReconcileSource(ctx, f.tenant.String(), source.String(), d.ID, f.screen.ID)
				if err == nil && result.Status != screening.ExecutionStatusSent {
					t.Errorf("unexpected projection: %s", result.Status)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
	})
}
