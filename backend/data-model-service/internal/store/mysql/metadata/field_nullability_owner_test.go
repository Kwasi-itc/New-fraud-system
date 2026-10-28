package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

// Start with an actually published native field, then execute a second v3 plan
// through the restricted installer before invoking the concrete update owner.
func verifyFieldNullabilityOwner(t *testing.T, ctx context.Context, db *sql.DB, database string, tenantID uuid.UUID, field datamodel.Field, name string, previous store.PhysicalDDLPlan, mapped []store.MappedPhysicalField, shared *installation.ExactKeyReadiness, installer installation.RecordInstallerPolicy, policies []store.PhysicalAccessPolicy, scenario string) {
	t.Helper()
	resultMode := strings.Contains(scenario, "_result_")
	var originalResult datamodel.Field
	schema, _ := mysqlstore.Identifier(database)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	planner, err := store.NewPhysicalPlanner(database, tenantID, store.PhysicalTable{TableID: field.TableID, Name: name}, mapped)
	if err != nil {
		t.Fatal(err)
	}
	unique := strings.Contains(scenario, "_native_unique_")
	nullable := strings.Contains(scenario, "_relax_") || unique
	var scoreColumn string
	for _, f := range mapped {
		if f.Field.ID == field.ID {
			scoreColumn = f.Components[0].Name
		}
	}
	insertRecord := "INSERT INTO " + schema + ".`" + name + "` (id,`" + previous.Creation.ObjectColumn + "`,`" + previous.Creation.UpdatedColumn + "`,`" + scoreColumn + "`) VALUES (?,?,?,?)"
	statement, err := planner.SetFieldNullable(field.ID, nullable)
	if unique {
		statement, err = planner.CreateFieldUniqueIndex(field.ID)
	}
	if err != nil {
		t.Fatal(err)
	}
	relax, err := planner.SetFieldNullable(field.ID, field.Nullable)
	if unique {
		relax, err = planner.DropFieldUniqueIndex(field.ID)
	}
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	step := statement.ReferenceStep()
	step.Before, err = migrations.Definition(ctx, conn, database, "TABLE", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, step.SQL); err != nil {
		t.Fatal(err)
	}
	step.After, err = migrations.Definition(ctx, conn, database, "TABLE", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, relax.ReferenceStep().SQL); err != nil {
		t.Fatal(err)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	plan, err := planner.BindRecordChanges(previous, []store.PhysicalStatement{statement}, []migrations.Step{step})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var epoch int64
	var version mysqlstore.UUID
	if err = db.QueryRowContext(ctx, "SELECT epoch,published_version FROM "+schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(field.TableID)).Scan(&epoch, &version); err != nil {
		t.Fatal(err)
	}
	actor := uuid.New()
	alias := " updated score "
	input := service.UpdateFieldInput{FieldID: field.ID, Nullable: &nullable, Alias: &alias}
	if unique {
		input.Nullable = nil
		input.IsUnique = &unique
	}
	expected := uuid.UUID(version)
	r := ports.PhysicalPublicationRequest{TableID: field.TableID, OperationID: uuid.New(), ActorKind: "api", ActorID: actor.String(), Condition: store.PublicationCondition{Mode: store.PublicationExactVersion, Version: expected}}
	r.RequestHash, err = service.PublicationRequestHash(service.PublicationRequestContext{TenantID: tenantID, TableID: field.TableID, ActorID: actor, Condition: r.Condition}, input)
	if err != nil {
		t.Fatal(err)
	}
	intents, err := store.NewDDLIntentStore(db, database, tenantID, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err = mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		_, e := intents.PrepareTx(ctx, tx, store.DDLIntent{TableID: field.TableID, OperationID: r.OperationID, RequestHash: r.RequestHash, ActorKind: r.ActorKind, ActorID: r.ActorID, ExpectedEpoch: epoch, ExpectedVersion: &expected, Shape: raw})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	account, _ := installer.Principal.SQLAccount()
	if nullable {
		exec(insertRecord, mysqlstore.UUID(uuid.New()), "retained-relaxation-record", mysqlstore.DateTime{Time: time.Now().UTC().Truncate(time.Microsecond)}, 42)
	}
	exec("ALTER USER " + account + " ACCOUNT UNLOCK")
	login, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS"), Database: database, User: installer.Principal.User, Password: "isolated-native-only", AllowInsecureTransport: true, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer login.Close()
	executor, err := store.NewPhysicalExecutor(login, database, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(scenario, "_populated") {
		// A non-null existing value is still outside the current empty-table
		// protocol. Never let MySQL implicitly rewrite a populated table here.
		var column string
		for _, f := range mapped {
			if f.Field.ID == field.ID {
				column = f.Components[0].Name
			}
		}
		exec("INSERT INTO "+schema+".`"+name+"` (id,`"+previous.Creation.ObjectColumn+"`,`"+previous.Creation.UpdatedColumn+"`,`"+column+"`) VALUES (?,?,?,?)", mysqlstore.UUID(uuid.New()), "retained-record", mysqlstore.DateTime{Time: time.Now().UTC().Truncate(time.Microsecond)}, 42)
		if e := executor.ExecuteSteps(ctx, field.TableID, r.OperationID); e == nil {
			t.Fatal("populated tightening accepted")
		}
		var journal, records int
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_schema_change_steps WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(r.OperationID)).Scan(&journal); e != nil || journal != 0 {
			t.Fatal("blocked operation journal", journal, e)
		}
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".`"+name+"` WHERE `"+column+"`=?", 42).Scan(&records); e != nil || records != 1 {
			t.Fatal("blocked operation changed records", records, e)
		}
		var gate string
		var nullable bool
		if e := db.QueryRowContext(ctx, "SELECT p.write_state,f.nullable FROM "+schema+".data_model_physical_tables p JOIN "+schema+".data_model_fields f ON f.tenant_id=p.tenant_id AND f.table_id=p.table_id WHERE p.tenant_id=? AND f.id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(field.ID)).Scan(&gate, &nullable); e != nil || gate != "closed" || !nullable {
			t.Fatal("blocked metadata/gate", gate, nullable, e)
		}
		if e := login.Close(); e != nil {
			t.Fatal(e)
		}
		exec("ALTER USER " + account + " ACCOUNT LOCK")
		return
	}
	if unique && (strings.HasSuffix(scenario, "_duplicates") || strings.HasSuffix(scenario, "_repair_commit")) {
		duplicateID := uuid.New()
		exec(insertRecord, mysqlstore.UUID(duplicateID), "duplicate-score", mysqlstore.DateTime{Time: time.Now().UTC().Truncate(time.Microsecond)}, 42)
		e := executor.ExecuteSteps(ctx, field.TableID, r.OperationID)
		var mysqlErr *driver.MySQLError
		if !errors.As(e, &mysqlErr) || mysqlErr.Number != 1062 {
			t.Fatal("duplicate unique index must fail", e)
		}
		var count int
		var gate string
		var flag bool
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".`"+name+"` WHERE `"+scoreColumn+"`=42").Scan(&count); e != nil || count != 2 {
			t.Fatal("duplicate data changed", count, e)
		}
		if e := db.QueryRowContext(ctx, "SELECT p.write_state,f.is_unique FROM "+schema+".data_model_physical_tables p JOIN "+schema+".data_model_fields f ON f.tenant_id=p.tenant_id AND f.table_id=p.table_id WHERE p.tenant_id=? AND f.id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(field.ID)).Scan(&gate, &flag); e != nil || gate != "closed" || flag {
			t.Fatal("failed index opened readiness", gate, flag, e)
		}
		if strings.HasSuffix(scenario, "_duplicates") {
			if e := login.Close(); e != nil {
				t.Fatal(e)
			}
			exec("ALTER USER " + account + " ACCOUNT LOCK")
			return
		}
		// Explicit fixture-only data correction, never automatic application repair.
		exec("UPDATE "+schema+".`"+name+"` SET `"+scoreColumn+"`=? WHERE id=?", 43, mysqlstore.UUID(duplicateID))
	}
	if err = executor.ExecuteSteps(ctx, field.TableID, r.OperationID); err != nil {
		t.Fatal(err)
	}
	if err = login.Close(); err != nil {
		t.Fatal(err)
	}
	exec("ALTER USER " + account + " ACCOUNT LOCK")
	if strings.HasSuffix(scenario, "_drift") {
		exec(relax.ReferenceStep().SQL)
	}
	denied := false
	localCalls := 0
	receiptLimited := strings.HasSuffix(scenario, "_receipt_limit")
	makeManager := func(pool *sql.DB) *store.PhysicalPublicationManager {
		limit := 1 << 20
		if receiptLimited {
			limit = 256
		}
		metadata, e := store.NewTransactionManager(pool, database, tenantID, 100, limit, func(context.Context, *sql.Tx, uuid.UUID) error { localCalls++; return nil }, nil)
		if e != nil {
			t.Fatal(e)
		}
		pub, e := store.NewPublicationStore(pool, database, 100, 100, 1<<20)
		if e != nil {
			t.Fatal(e)
		}
		manager, e := store.NewPhysicalPublicationManager(metadata, pub, store.PhysicalTable{TableID: field.TableID, Name: name}, policies, func(_ context.Context, tenant uuid.UUID, request ports.PhysicalPublicationRequest) error {
			if denied || tenant != tenantID || request.ActorID != r.ActorID || request.TableID != r.TableID {
				return errors.New("denied")
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
		manager, e = manager.WithCreationReadiness(shared, installer)
		if e != nil {
			t.Fatal(e)
		}
		return manager
	}
	manager := makeManager(db)
	ids := &creationOwnerIDs{}
	if strings.HasSuffix(scenario, "_rollback") {
		ids.fail = 2
	}
	owner := service.NewFieldService(nil, nil, nil, nil, nil, nil, nil, nil, nil, ids, creationOwnerClock{})
	publish := owner.PublishPreparedNullabilityUpdate
	if unique {
		publish = owner.PublishPreparedUniqueEnable
	}
	if resultMode {
		publish = func(ctx context.Context, tenant uuid.UUID, in service.UpdateFieldInput, request ports.PhysicalPublicationRequest, m ports.PhysicalPublicationManager) (contract.Publication, error) {
			typed := m.(ports.PhysicalFieldPublicationManager)
			var saved datamodel.Field
			var e error
			if unique {
				saved, e = owner.PublishPreparedUniqueResult(ctx, tenant, in, request, typed)
			} else {
				saved, e = owner.PublishPreparedNullabilityResult(ctx, tenant, in, request, typed)
			}
			if e != nil {
				if saved.ID != uuid.Nil {
					t.Fatal("provisional Field escaped")
				}
				return contract.Publication{}, e
			}
			if originalResult.ID != uuid.Nil && saved != originalResult {
				t.Fatal("original Field changed")
			}
			originalResult = saved
			return recoveredPhysicalResultPublication(t, ctx, db, database, tenant, request, saved, in.FieldID)
		}
	}
	selected := manager
	var observed atomic.Bool
	if strings.HasSuffix(scenario, "_lost_commit") {
		selected = makeManager(deliveryFaultDB(t, database, &observed))
	}
	result, err := publish(ctx, tenantID, input, r, selected)
	if strings.HasSuffix(scenario, "_lost_commit") {
		var ambiguous *mysqlstore.CommitError
		if !errors.As(err, &ambiguous) || !observed.Load() || result.Version != uuid.Nil {
			t.Fatal("lost nullability commit", err)
		}
		sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + field.TableID.String()))
		lock := fmt.Sprintf("data_model_ddl_%x", sum[:24])
		conn, e := db.Conn(ctx)
		if e != nil {
			t.Fatal(e)
		}
		var acquired int
		if e = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,5)", lock).Scan(&acquired); e != nil || acquired != 1 {
			t.Fatal("old owner lock", e)
		}
		if e = conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lock).Scan(&acquired); e != nil || acquired != 1 {
			t.Fatal(e)
		}
		if e = conn.Close(); e != nil {
			t.Fatal(e)
		}
		result, err = publish(ctx, tenantID, input, r, manager)
	}
	if receiptLimited && (!errors.Is(err, store.ErrSnapshotLimit) || localCalls != 1 || ids.calls != 2) {
		t.Fatal("receipt failure must follow owner effects", err, localCalls, ids.calls)
	}
	want := 0
	if strings.HasSuffix(scenario, "_commit") {
		want = 1
		if err != nil || result.Version == uuid.Nil || result.Version == expected || ids.calls != 2 || localCalls != 1 {
			t.Fatal("nullability owner", err, ids.calls, localCalls)
		}
		replay, e := publish(ctx, tenantID, input, r, manager)
		if e != nil || replay.Version != result.Version || ids.calls != 2 || localCalls != 1 {
			t.Fatal("nullability replay", e)
		}
		denied = true
		if _, e = publish(ctx, tenantID, input, r, manager); e == nil {
			t.Fatal("revoked replay accepted")
		}
		denied = false
		changed := input
		flag := !nullable
		changed.Nullable = &flag
		if unique {
			changed.Nullable = nil
			changed.IsUnique = &flag
		}
		if _, e = publish(ctx, tenantID, changed, r, manager); e == nil {
			t.Fatal("changed command accepted")
		}
	} else if err == nil || result.Version != uuid.Nil {
		t.Fatal("unsafe nullability publication", err)
	}
	var gotNullable, gotUnique bool
	var gotAlias, gate string
	var audits, revisions int
	if err = db.QueryRowContext(ctx, "SELECT nullable,is_unique,alias FROM "+schema+".data_model_fields WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(field.ID)).Scan(&gotNullable, &gotUnique, &gotAlias); err != nil {
		t.Fatal(err)
	}
	expectedNullable := field.Nullable
	if want == 1 {
		expectedNullable = nullable
	}
	if gotUnique != (unique && want == 1) || gotNullable != expectedNullable || (want == 1 && gotAlias != "updated score") || (want == 0 && gotAlias != "") {
		t.Fatal("owner metadata rollback", gotNullable, gotAlias)
	}
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_schema_change_log WHERE tenant_id=? AND resource_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(field.ID)).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_tenant_schema_migrations WHERE tenant_id=?", mysqlstore.UUID(tenantID)).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if audits != 1+want || revisions != 2+want {
		t.Fatal("atomic nullability effects", audits, revisions)
	}
	if err = db.QueryRowContext(ctx, "SELECT write_state FROM "+schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(field.TableID)).Scan(&gate); err != nil {
		t.Fatal(err)
	}
	if (want == 1 && gate != "open") || (want == 0 && gate != "closed") {
		t.Fatal("unsafe gate", gate)
	}
	if want == 1 {
		_, err := db.ExecContext(ctx, insertRecord, mysqlstore.UUID(uuid.New()), "nullability-probe", mysqlstore.DateTime{Time: time.Now().UTC().Truncate(time.Microsecond)}, nil)
		if (nullable && err != nil) || (!nullable && err == nil) {
			t.Fatal("physical enforcement disagrees with publication", nullable, err)
		}
		if nullable {
			var count int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".`"+name+"` WHERE `"+scoreColumn+"`=42").Scan(&count); err != nil || count != 1 {
				t.Fatal("relaxation lost existing record", count, err)
			}
		}
		if unique {
			// Nullable unique fields allow multiple NULLs, but never equal values.
			exec(insertRecord, mysqlstore.UUID(uuid.New()), "second-null", mysqlstore.DateTime{Time: time.Now().UTC().Truncate(time.Microsecond)}, nil)
			_, e := db.ExecContext(ctx, insertRecord, mysqlstore.UUID(uuid.New()), "duplicate-probe", mysqlstore.DateTime{Time: time.Now().UTC().Truncate(time.Microsecond)}, 42)
			var mysqlErr *driver.MySQLError
			if !errors.As(e, &mysqlErr) || mysqlErr.Number != 1062 {
				t.Fatal("published numeric uniqueness not enforced", e)
			}
			_, e = db.ExecContext(ctx, "UPDATE "+schema+".`"+name+"` SET `"+scoreColumn+"`=? WHERE `"+scoreColumn+"` IS NULL", 42)
			if !errors.As(e, &mysqlErr) || mysqlErr.Number != 1062 {
				t.Fatal("duplicate update accepted", e)
			}
			var nulls int
			if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".`"+name+"` WHERE `"+scoreColumn+"` IS NULL").Scan(&nulls); e != nil || nulls != 2 {
				t.Fatal("failed duplicate update changed records", nulls, e)
			}
		}
	}
	if resultMode {
		verifyPhysicalFieldReceipt(t, ctx, db, database, tenantID, field.TableID, r, manager, originalResult, want)
	}
	if receiptLimited {
		receiptLimited = false
		healthy := makeManager(db)
		if _, e := publish(ctx, tenantID, input, r, healthy); e != nil {
			t.Fatal("receipt-only recovery", e)
		}
		if localCalls != 2 || ids.calls != 4 {
			t.Fatal("receipt recovery effects", localCalls, ids.calls)
		}
		if originalResult.Nullable != nullable || originalResult.IsUnique != unique {
			t.Fatal("wrong recovered enforcement result", originalResult)
		}
		verifyPhysicalFieldReceipt(t, ctx, db, database, tenantID, field.TableID, r, healthy, originalResult, 1)
	}
}
