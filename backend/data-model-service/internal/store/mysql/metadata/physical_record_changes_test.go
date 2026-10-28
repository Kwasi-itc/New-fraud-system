package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func verifyGuardedNativeChange(t *testing.T, ctx context.Context, db *sql.DB, database string, tenantID, tableID uuid.UUID, name string, original store.PhysicalDDLPlan, mapped []store.MappedPhysicalField, shared *installation.ExactKeyReadiness, installer installation.RecordInstallerPolicy, policies []store.PhysicalAccessPolicy, scenario string) {
	t.Helper()
	schema, _ := mysqlstore.Identifier(database)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	fieldID := uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	field := datamodel.Field{ID: fieldID, TenantID: tenantID, TableID: tableID, Name: "score", DataType: datamodel.DataTypeInt, Nullable: true, CreatedAt: at, UpdatedAt: at}
	if strings.Contains(scenario, "_nullability_relax_") {
		field.Nullable = false
	}
	if strings.Contains(scenario, "_unique_float_") {
		field.DataType = datamodel.DataTypeFloat
	}
	column := "field_score_" + strings.ReplaceAll(fieldID.String(), "-", "") + "_value"
	mapped = append(mapped, store.MappedPhysicalField{Field: field, Components: []store.PhysicalField{{FieldID: fieldID, Name: column, Component: "value"}}})
	planner, err := store.NewPhysicalPlanner(database, tenantID, store.PhysicalTable{TableID: tableID, Name: name}, mapped)
	if err != nil {
		t.Fatal(err)
	}
	statement, err := planner.AddFields([]uuid.UUID{fieldID})
	if err != nil {
		t.Fatal(err)
	}
	indexID := uuid.New()
	index, e := planner.CreateIndex(indexID, []uuid.UUID{fieldID}, false)
	if e != nil {
		t.Fatal(e)
	}
	statements := []store.PhysicalStatement{statement, index}
	nullabilityOwner := strings.HasPrefix(scenario, "service_allocated_native_nullability_") || strings.HasPrefix(scenario, "service_allocated_native_unique_")
	fieldOwner := strings.HasPrefix(scenario, "service_allocated_native_field_") || nullabilityOwner
	if fieldOwner {
		statements = statements[:1]
	}
	var reference []migrations.Step
	// The entire database is an isolated reference fixture. Capture this known
	// source extension and restore the exact initial shape before execution.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		step := statement.ReferenceStep()
		step.Before, err = migrations.Definition(ctx, conn, database, "TABLE", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, step.SQL); err != nil {
			t.Fatal(err)
		}
		step.After, err = migrations.Definition(ctx, conn, database, "TABLE", name)
		if err != nil {
			t.Fatal(err)
		}
		reference = append(reference, step)
	}
	drop, e := planner.DropIndex(indexID)
	if e != nil {
		t.Fatal(e)
	}
	if !fieldOwner {
		if _, err := conn.ExecContext(ctx, drop.ReferenceStep().SQL); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.ExecContext(ctx, "ALTER TABLE "+schema+".`"+name+"` DROP COLUMN `"+column+"`"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	step := reference[0]
	plan, err := planner.BindRecordChanges(original, statements, reference)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var epoch int64
	var version mysqlstore.UUID
	if err := db.QueryRowContext(ctx, "SELECT epoch,published_version FROM "+schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&epoch, &version); err != nil {
		t.Fatal(err)
	}
	expected := uuid.UUID(version)
	operation := uuid.New()
	hash := [32]byte{9}
	actor := uuid.NewString()
	if fieldOwner {
		hash, err = service.PreparedFieldCreationRequestHash(service.PublicationRequestContext{TenantID: tenantID, TableID: tableID, ActorID: uuid.MustParse(actor), Condition: store.PublicationCondition{Mode: store.PublicationExactVersion, Version: expected}}, fieldID, service.CreateFieldInput{TableID: tableID, Name: field.Name, DataType: field.DataType, Nullable: field.Nullable})
		if err != nil {
			t.Fatal(err)
		}
	}
	intents, err := store.NewDDLIntentStore(db, database, tenantID, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		var locked mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+schema+".data_model_tables WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&locked); err != nil {
			return err
		}
		if _, err := intents.PrepareTx(ctx, tx, store.DDLIntent{TableID: tableID, OperationID: operation, RequestHash: hash, ActorKind: "api", ActorID: actor, ExpectedEpoch: epoch, ExpectedVersion: &expected, Shape: raw}); err != nil {
			return err
		}
		repos, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 1<<20)
		if err != nil {
			return err
		}
		if err := repos.Fields.Create(ctx, field); err != nil {
			return err
		}
		registry, err := store.NewPhysicalRegistry(tx, database, tenantID, 100)
		if err != nil {
			return err
		}
		return registry.RegisterFields(ctx, tableID, []uuid.UUID{fieldID})
	}); err != nil {
		t.Fatal(err)
	}
	// Trigger inspection authority belongs to a temporary installer, never the
	// long-lived native DDL role. It cannot install another principal's definer.
	native := installation.RecordInstallerPolicy{Principal: installation.Definer{User: "na_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24], Host: "%"}, Table: name, NativeChanges: true}
	account, _ := native.Principal.SQLAccount()
	exec("CREATE USER " + account + " IDENTIFIED BY 'isolated-native-only'")
	defer func() {
		if _, err := db.Exec("DROP USER " + account); err != nil {
			t.Error(err)
		}
	}()
	grants, err := native.ActiveGrants(database)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range grants {
		exec(q)
	}
	inspection, e := db.Conn(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e := native.VerifyActive(ctx, inspection, database); e != nil {
		t.Fatal(e)
	}
	if e := inspection.Close(); e != nil {
		t.Fatal(e)
	}
	login, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS"), Database: database, User: native.Principal.User, Password: "isolated-native-only", AllowInsecureTransport: true, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer login.Close()
	executor, err := store.NewPhysicalExecutor(login, database, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	waitReleased := func() {
		t.Helper()
		sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + tableID.String()))
		lock := fmt.Sprintf("data_model_ddl_%x", sum[:24])
		conn, e := db.Conn(ctx)
		if e != nil {
			t.Fatal(e)
		}
		var acquired int
		if e := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,5)", lock).Scan(&acquired); e != nil || acquired != 1 {
			t.Fatal("old session lock", e)
		}
		if e := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lock).Scan(&acquired); e != nil || acquired != 1 {
			t.Fatal(e)
		}
		if e := conn.Close(); e != nil {
			t.Fatal(e)
		}
	}
	if scenario == "service_allocated_native_guard_before" {
		exec("DROP TRIGGER " + schema + ".`" + original.Steps[1].Name + "`")
		if err := executor.ExecuteSteps(ctx, tableID, operation); !errors.Is(err, migrations.ErrDrift) {
			t.Fatal("missing retained guard accepted", err)
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_schema_change_steps WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(operation)).Scan(&count); err != nil || count != 0 {
			t.Fatal("DDL journal started before guard verification", err, count)
		}
		return
	}
	if scenario == "service_allocated_native_missing_alter" {
		exec("REVOKE ALTER ON " + schema + ".`" + name + "` FROM " + account)
		if err := executor.ExecuteSteps(ctx, tableID, operation); err == nil {
			t.Fatal("ALTER without privilege")
		}
		for _, q := range grants {
			exec(q)
		}
	}
	if scenario == "service_allocated_native_lost_ack" {
		var observed atomic.Bool
		faultDB := physicalFaultDBAs(t, database, step.SQL, &observed, native.Principal.User, "isolated-native-only")
		fault, e := store.NewPhysicalExecutor(faultDB, database, tenantID)
		if e != nil {
			t.Fatal(e)
		}
		if e := fault.ExecuteSteps(ctx, tableID, operation); e == nil || !observed.Load() {
			t.Fatal("lost native ALTER acknowledgement", e)
		}
		if e := faultDB.Close(); e != nil {
			t.Fatal(e)
		}
		waitReleased()
	}
	if err := executor.ExecuteSteps(ctx, tableID, operation); err != nil {
		t.Fatal("restricted guarded ALTER", err)
	}
	if err := executor.ExecuteSteps(ctx, tableID, operation); err != nil {
		t.Fatal("guarded ALTER replay", err)
	}
	if scenario != "service_allocated_native_active_installer" {
		if err := login.Close(); err != nil {
			t.Fatal(err)
		}
		exec("ALTER USER " + account + " ACCOUNT LOCK")
	}
	if scenario == "service_allocated_native_guard_after" {
		exec("DROP TRIGGER " + schema + ".`" + original.Steps[1].Name + "`")
	}
	verifier, err := store.NewPhysicalExecutor(db, database, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err = verifier.WithCreationReadiness(shared, native)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := store.NewPublicationStore(db, database, 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	request := store.PhysicalPublicationRequest{TableID: tableID, Operation: store.PublicationOperation{ID: operation, RequestHash: hash, Condition: store.PublicationCondition{Mode: store.PublicationExactVersion, Version: expected}}, ActorKind: "api", ActorID: actor}
	if fieldOwner {
		creationScenario := scenario
		if nullabilityOwner {
			creationScenario = "service_allocated_native_field_commit"
		}
		verifyPreparedFieldOwner(t, ctx, db, database, tenantID, field, request, name, policies, shared, native, creationScenario)
		if nullabilityOwner {
			verifyFieldNullabilityOwner(t, ctx, db, database, tenantID, field, name, plan, mapped, shared, native, policies, scenario)
		}
		return
	}
	calls := 0
	authorize := func(context.Context, uuid.UUID, store.PhysicalPublicationRequest) error { return nil }
	owner := func(tx *sql.Tx) error {
		calls++
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_fields WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&count); err != nil {
			return err
		}
		if count != 3 {
			return errors.New("owner field snapshot mismatch")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE "+schema+".data_model_tables SET alias='native published' WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)); err != nil {
			return err
		}
		if scenario == "service_allocated_native_owner_rollback" {
			return errors.New("native owner rollback")
		}
		switch scenario {
		case "service_allocated_native_metadata_mismatch":
			_, err := tx.ExecContext(ctx, "UPDATE "+schema+".data_model_fields SET nullable=FALSE WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(fieldID))
			return err
		case "service_allocated_native_extra_field":
			repos, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 1<<20)
			if err != nil {
				return err
			}
			return repos.Fields.Create(ctx, datamodel.Field{ID: uuid.New(), TenantID: tenantID, TableID: tableID, Name: "extra", DataType: datamodel.DataTypeString, Nullable: true, CreatedAt: at, UpdatedAt: at})
		case "service_allocated_native_missing_mapping":
			_, err := tx.ExecContext(ctx, "DELETE FROM "+schema+".data_model_physical_fields WHERE tenant_id=? AND table_id=? AND field_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(fieldID))
			return err
		}
		return nil
	}
	selected := verifier
	var observed atomic.Bool
	if scenario == "service_allocated_native_lost_commit" {
		fault, e := store.NewPhysicalExecutor(deliveryFaultDB(t, database, &observed), database, tenantID)
		if e != nil {
			t.Fatal(e)
		}
		selected, e = fault.WithCreationReadiness(shared, native)
		if e != nil {
			t.Fatal(e)
		}
	}
	published, err := selected.PublishVerifiedOperation(ctx, publication, request, policies, authorize, owner)
	if scenario == "service_allocated_native_lost_commit" {
		var ambiguous *mysqlstore.CommitError
		if !errors.As(err, &ambiguous) || !observed.Load() || published.Version != uuid.Nil {
			t.Fatal("native ambiguous publication", err)
		}
		waitReleased()
		published, err = verifier.PublishVerifiedOperation(ctx, publication, request, policies, authorize, owner)
	}
	metadataFailure := scenario == "service_allocated_native_metadata_mismatch" || scenario == "service_allocated_native_extra_field" || scenario == "service_allocated_native_missing_mapping"
	if scenario == "service_allocated_native_guard_after" || scenario == "service_allocated_native_active_installer" || scenario == "service_allocated_native_owner_rollback" || metadataFailure {
		if err == nil || published.Version != uuid.Nil {
			t.Fatal("unsafe native publication", err)
		}
		wantCalls := 0
		if scenario == "service_allocated_native_owner_rollback" || metadataFailure {
			wantCalls = 1
		}
		if calls != wantCalls {
			t.Fatal("native owner called on invalid readiness", calls)
		}
		var gate, alias string
		if e := db.QueryRowContext(ctx, "SELECT p.write_state,t.alias FROM "+schema+".data_model_physical_tables p JOIN "+schema+".data_model_tables t ON t.tenant_id=p.tenant_id AND t.id=p.table_id WHERE p.tenant_id=? AND p.table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&gate, &alias); e != nil || gate != "closed" || alias != "" {
			t.Fatal("partial native publication", e, gate, alias)
		}
		if metadataFailure {
			for _, relation := range []string{"data_model_fields", "data_model_physical_fields"} {
				var count int
				if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+relation+" WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&count); e != nil || count != 3 {
					t.Fatal("metadata effects survived rollback", e, relation, count)
				}
			}
			var nullable bool
			if e := db.QueryRowContext(ctx, "SELECT nullable FROM "+schema+".data_model_fields WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(fieldID)).Scan(&nullable); e != nil || !nullable {
				t.Fatal("metadata type effects survived rollback", e)
			}
		}
		return
	}
	if err != nil || published.Version == uuid.Nil || published.Version == expected || calls != 1 {
		t.Fatal("guarded native publication", err, calls)
	}
	exec("INSERT INTO "+schema+".`"+name+"` (id,`"+original.Creation.ObjectColumn+"`,`"+original.Creation.UpdatedColumn+"`,`"+column+"`) VALUES (?,?,?,?)", mysqlstore.UUID(uuid.New()), "after native alteration", at, 42)
	_, err = db.ExecContext(ctx, "INSERT INTO "+schema+".`"+name+"` (id,`"+original.Creation.ObjectColumn+"`,`"+original.Creation.UpdatedColumn+"`) VALUES (?,?,?)", mysqlstore.UUID(uuid.New()), "after native alteration", at)
	mysqlError(t, err, 1062)
}
