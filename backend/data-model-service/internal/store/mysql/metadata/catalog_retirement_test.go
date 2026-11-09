package metadata_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCatalogRetirementRecovery(t *testing.T) {
	testPhysicalCreationPublication(t, true, false,
		"service_allocated_native_field_delete_rollback", "service_allocated_native_field_delete_lost_ack", "service_allocated_native_field_delete_lost_commit",
		"service_allocated_native_field_enum_rollback", "service_allocated_native_field_enum_lost_commit")
}

func verifyCatalogRetirement(t *testing.T, ctx context.Context, db *sql.DB, database string, tenant uuid.UUID, field datamodel.Field, name string, previous store.PhysicalDDLPlan, mapped []store.MappedPhysicalField, shared *installation.ExactKeyReadiness, installer installation.RecordInstallerPolicy, policies []store.PhysicalAccessPolicy, scenario string) {
	t.Helper()
	schema, _ := mysqlstore.Identifier(database)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	actor := uuid.New()
	table := field.TableID
	var version mysqlstore.UUID
	var epoch int64
	if err := db.QueryRowContext(ctx, "SELECT published_version,epoch FROM "+schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table)).Scan(&version, &epoch); err != nil {
		t.Fatal(err)
	}
	makeStores := func(pool *sql.DB) (*store.TransactionManager, *store.PublicationStore) {
		m, e := store.NewTransactionManager(pool, database, tenant, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID) error { return nil }, nil)
		if e != nil {
			t.Fatal(e)
		}
		p, e := store.NewPublicationStore(pool, database, 100, 100, 1<<20)
		if e != nil {
			t.Fatal(e)
		}
		return m, p
	}
	condition := store.PublicationCondition{Mode: store.PublicationExactVersion, Version: uuid.UUID(version)}
	if strings.Contains(scenario, "_enum_") {
		m, p := makeStores(db)
		manager, e := store.NewMetadataPublicationManager(m, p, func(context.Context, ports.MetadataPublicationRequest) error { return nil })
		if e != nil {
			t.Fatal(e)
		}
		seed := datamodel.FieldEnumValue{ID: uuid.New(), TenantID: tenant, FieldID: field.ID, Value: "1", Label: "One", CreatedAt: time.Now().UTC().Truncate(time.Microsecond), UpdatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		r := ports.MetadataPublicationRequest{TenantID: tenant, TableID: table, ActorID: actor, OperationID: uuid.New(), RequestHash: [32]byte{1}, Condition: condition}
		if _, e = manager.PublishCatalogField(ctx, r, field.ID, func(tx ports.MutationStore) error {
			field.IsEnum = true
			if e := tx.Fields().Update(ctx, field); e != nil {
				return e
			}
			return tx.FieldEnumValues().Create(ctx, seed)
		}); e != nil {
			t.Fatal(e)
		}
		if e = db.QueryRowContext(ctx, "SELECT published_version FROM "+schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table)).Scan(&version); e != nil {
			t.Fatal(e)
		}
		r.OperationID = uuid.New()
		r.Condition.Version = uuid.UUID(version)
		r.RequestHash = [32]byte{2}
		owner := service.NewFieldEnumValueService(nil, nil, nil, nil, &creationOwnerIDs{}, creationOwnerClock{})
		calls := 0
		rollback := strings.HasSuffix(scenario, "rollback")
		effects := func(tx ports.MutationStore) (datamodel.FieldEnumValue, error) {
			calls++
			v, e := owner.ApplyCatalogMutation(ctx, tx, service.UpdateFieldEnumValueInput{EnumValueID: seed.ID}, true, tenant, table)
			if e == nil && rollback {
				return v, errors.New("injected owner failure")
			}
			return v, e
		}
		selected := manager
		var observed atomic.Bool
		if strings.HasSuffix(scenario, "lost_commit") {
			m, p = makeStores(deliveryFaultDB(t, database, &observed))
			selected, e = store.NewMetadataPublicationManager(m, p, func(context.Context, ports.MetadataPublicationRequest) error { return nil })
			if e != nil {
				t.Fatal(e)
			}
		}
		_, e = selected.PublishEnum(ctx, r, seed.ID, effects)
		if e == nil {
			t.Fatal("fault did not fail")
		}
		if rollback {
			var count int
			if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_field_enum_values WHERE id=?", mysqlstore.UUID(seed.ID)).Scan(&count); e != nil || count != 1 {
				t.Fatal("rollback lost enum", count, e)
			}
			rollback = false
		} else {
			var ambiguous *mysqlstore.CommitError
			if !errors.As(e, &ambiguous) || !observed.Load() {
				t.Fatal("missing commit loss", e)
			}
		}
		got, e := manager.PublishEnum(ctx, r, seed.ID, effects)
		if e != nil || got.ID != seed.ID {
			t.Fatal(got, e)
		}
		before := calls
		if _, e = manager.PublishEnum(ctx, r, seed.ID, effects); e != nil || calls != before {
			t.Fatal("duplicate enum effects", e)
		}
		foreign := r
		foreign.TenantID = uuid.New()
		if _, e = manager.PublishEnum(ctx, foreign, seed.ID, effects); e == nil {
			t.Fatal("foreign tenant accepted")
		}
		return
	}
	planner, e := store.NewPhysicalPlanner(database, tenant, store.PhysicalTable{TableID: table, Name: name}, mapped)
	if e != nil {
		t.Fatal(e)
	}
	statement, e := planner.DropField(field.ID)
	if e != nil {
		t.Fatal(e)
	}
	conn, e := db.Conn(ctx)
	if e != nil {
		t.Fatal(e)
	}
	step := statement.ReferenceStep()
	step.Before, e = migrations.Definition(ctx, conn, database, step.Kind, step.Name)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = conn.ExecContext(ctx, step.SQL); e != nil {
		t.Fatal(e)
	}
	step.After, e = migrations.Definition(ctx, conn, database, step.Kind, step.Name)
	if e != nil {
		t.Fatal(e)
	}
	// Restore only this generated, empty disposable fixture after capturing DDL.
	var definition map[string]string
	if e = json.Unmarshal([]byte(previous.FinalTableDefinition()), &definition); e != nil {
		t.Fatal(e)
	}
	if _, e = conn.ExecContext(ctx, "DROP TABLE "+schema+".`"+name+"`"); e != nil {
		t.Fatal(e)
	}
	if _, e = conn.ExecContext(ctx, definition["Create Table"]); e != nil {
		t.Fatal(e)
	}
	for _, g := range previous.ActiveGuards() {
		if _, e = conn.ExecContext(ctx, g.SQL); e != nil {
			t.Fatal(e)
		}
	}
	if e = conn.Close(); e != nil {
		t.Fatal(e)
	}
	plan, e := planner.BindRecordChanges(previous, []store.PhysicalStatement{statement}, []migrations.Step{step})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(plan)
	operation := uuid.New()
	input := service.DeleteFieldInput{FieldID: field.ID}
	hash, e := service.PublicationRequestHash(service.PublicationRequestContext{TenantID: tenant, TableID: table, ActorID: actor, Condition: condition}, input)
	if e != nil {
		t.Fatal(e)
	}
	intents, e := store.NewDDLIntentStore(db, database, tenant, 1<<20)
	if e != nil {
		t.Fatal(e)
	}
	v := uuid.UUID(version)
	e = mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		if _, e := store.CheckFieldDeletion(ctx, tx, database, tenant, table, field.ID); e != nil {
			return e
		}
		_, e := intents.PrepareTx(ctx, tx, store.DDLIntent{TableID: table, OperationID: operation, RequestHash: hash, ActorKind: "api", ActorID: actor.String(), ExpectedEpoch: epoch, ExpectedVersion: &v, Shape: raw})
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	account, _ := installer.Principal.SQLAccount()
	exec("ALTER USER " + account + " ACCOUNT UNLOCK")
	var observed atomic.Bool
	pool := physicalFaultDBAs(t, database, step.SQL, &observed, installer.Principal.User, "isolated-native-only")
	executor, e := store.NewPhysicalExecutor(pool, database, tenant)
	if e != nil {
		t.Fatal(e)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := executor.ExecuteSteps(canceled, table, operation); err == nil || observed.Load() {
		t.Fatal("canceled execution changed physical schema", err)
	}
	e = executor.ExecuteSteps(ctx, table, operation)
	if !observed.Load() || e == nil {
		t.Fatal("lost DDL acknowledgement not injected", e)
	}
	if e = executor.ExecuteSteps(ctx, table, operation); e != nil {
		t.Fatal(e)
	}
	if e = pool.Close(); e != nil {
		t.Fatal(e)
	}
	exec("ALTER USER " + account + " ACCOUNT LOCK")
	request := ports.PhysicalPublicationRequest{TableID: table, OperationID: operation, RequestHash: hash, Condition: condition, ActorKind: "api", ActorID: actor.String()}
	makeManager := func(pool *sql.DB) *store.PhysicalPublicationManager {
		m, p := makeStores(pool)
		v, e := store.NewPhysicalPublicationManager(m, p, store.PhysicalTable{TableID: table, Name: name}, policies, func(_ context.Context, id uuid.UUID, r ports.PhysicalPublicationRequest) error {
			if id != tenant || r != request {
				return errors.New("scope mismatch")
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
		v, e = v.WithCreationReadiness(shared, installer)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	owner := service.NewFieldService(nil, nil, nil, nil, nil, nil, nil, nil, nil, &creationOwnerIDs{}, creationOwnerClock{})
	calls := 0
	rollback := strings.HasSuffix(scenario, "rollback")
	effects := func(tx ports.MutationStore) error {
		calls++
		e := owner.ApplyPreparedDeletion(ctx, tx, input, tenant, table)
		if e == nil && rollback {
			return errors.New("injected owner failure")
		}
		return e
	}
	manager := makeManager(db)
	foreign := request
	foreign.TableID = uuid.New()
	if err := manager.PublishFieldDeletion(ctx, foreign, field.ID, effects); err == nil {
		t.Fatal("foreign table accepted")
	}
	selected := manager
	observed.Store(false)
	if strings.HasSuffix(scenario, "lost_commit") {
		selected = makeManager(deliveryFaultDB(t, database, &observed))
	}
	e = selected.PublishFieldDeletion(ctx, request, field.ID, effects)
	if rollback {
		if e == nil {
			t.Fatal("rollback accepted")
		}
		var count int
		if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_physical_fields WHERE field_id=?", mysqlstore.UUID(field.ID)).Scan(&count); e != nil || count != 1 {
			t.Fatal("rollback lost mapping", count, e)
		}
		rollback = false
	} else if strings.HasSuffix(scenario, "lost_commit") {
		var ambiguous *mysqlstore.CommitError
		if !errors.As(e, &ambiguous) || !observed.Load() {
			t.Fatal("missing commit loss", e)
		}
	} else if e != nil {
		t.Fatal(e)
	}
	if e = manager.PublishFieldDeletion(ctx, request, field.ID, effects); e != nil {
		t.Fatal(e)
	}
	before := calls
	if e = manager.PublishFieldDeletion(ctx, request, field.ID, effects); e != nil || calls != before {
		t.Fatal("duplicate deletion effects", e)
	}
}
