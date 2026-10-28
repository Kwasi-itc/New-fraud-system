package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func verifyDisplayOwner(t *testing.T, ctx context.Context, db *sql.DB, database string, settings *store.DisplayStore, publication *store.PublicationStore, tenantID, table, actor, operation uuid.UUID, initial contract.Publication, input display.SaveRequest, scenario string) {
	t.Helper()
	resultMode := strings.HasPrefix(scenario, "service_result_owner_")
	if resultMode {
		scenario = "service_" + strings.TrimPrefix(scenario, "service_result_owner_")
	}
	baseline := map[string]int{}
	for _, relation := range []string{"data_model_index_jobs", "data_model_index_deliveries", "data_model_tenant_schema_migrations", "data_model_schema_change_log"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+relation+" WHERE tenant_id=?", mysqlstore.UUID(tenantID)).Scan(&count); err != nil {
			t.Fatal(err)
		}
		baseline[relation] = count
	}
	var revoked atomic.Bool
	var localCalls atomic.Int32
	denied := errors.New("display owner denied")
	makeManager := func(pool *sql.DB) *store.DisplayPublicationManager {
		queue, err := store.NewIndexDeliveryStore(pool, database, "display-owner", 3, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if scenario == "service_missing_queue" {
			queue = nil
		}
		metadata, err := store.NewTransactionManager(pool, database, tenantID, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID) error {
			localCalls.Add(1)
			if scenario == "service_local_denial" {
				return denied
			}
			return nil
		}, queue)
		if err != nil {
			t.Fatal(err)
		}
		s, err := store.NewDisplayStore(pool, database, tenantID, 100, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		p, err := store.NewPublicationStore(pool, database, 100, 100, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		m, err := store.NewDisplayPublicationManager(metadata, s, p, func(ctx context.Context, scope, target, user uuid.UUID) error {
			if revoked.Load() || scope != tenantID || target != table || user != actor {
				return denied
			}
			return db.PingContext(ctx) // Must precede the owner transaction.
		})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	ownerPool := db
	var restrictedAccount string
	var restrictedGrants []string
	if scenario == "service_restricted" {
		principal := installation.Definer{User: "display_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24], Host: "%"}
		account, e := principal.SQLAccount()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.ExecContext(ctx, "CREATE USER "+account+" IDENTIFIED BY 'isolated-display-only'"); e != nil {
			t.Fatal(e)
		}
		defer func() {
			if _, e := db.Exec("DROP USER " + account); e != nil {
				t.Error(e)
			}
		}()
		policy, e := installation.DisplayOwnerPolicy(principal, false)
		if resultMode {
			policy, e = installation.DisplayResultOwnerPolicy(principal, false)
		}
		if e != nil {
			t.Fatal(e)
		}
		grants, e := installation.GrantStatements(database, policy)
		if e != nil {
			t.Fatal(e)
		}
		for _, q := range grants {
			if _, e = db.ExecContext(ctx, q); e != nil {
				t.Fatal(e)
			}
		}
		restrictedAccount, restrictedGrants = account, grants
		conn, e := db.Conn(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if e = installation.VerifyAccountPolicy(ctx, conn, database, policy); e != nil {
			t.Fatal(e)
		}
		if e = conn.Close(); e != nil {
			t.Fatal(e)
		}
		ownerPool, e = mysqlstore.Open(ctx, mysqlstore.Config{Address: os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS"), Database: database, User: principal.User, Password: "isolated-display-only", AllowInsecureTransport: true, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
		if e != nil {
			t.Fatal(e)
		}
		defer ownerPool.Close()
		for _, q := range []string{"SELECT * FROM data_model_exact_keys", "UPDATE data_model_physical_tables SET write_state='open'", "DELETE FROM data_model_publication_operations", "UPDATE data_model_fields SET nullable=TRUE", "CREATE TABLE forbidden_display_ddl (id INT)"} {
			if _, e := ownerPool.ExecContext(ctx, q); e == nil {
				t.Fatal("display principal bypass", q)
			}
		}
	}
	manager := makeManager(ownerPool)
	ids := &creationOwnerIDs{}
	if scenario == "service_revision_failure" {
		ids.fail = 2
	}
	owner := service.NewTableService(nil, nil, nil, nil, nil, nil, nil, nil, ids, creationOwnerClock{})
	// Compare immutable original snapshots for every operation, including
	// concurrent and historical calls, without sharing mutable caller results.
	var resultMu sync.Mutex
	originalResults := map[uuid.UUID]display.Settings{}
	publish := func(ctx context.Context, request display.PublicationRequest, manager *store.DisplayPublicationManager) (contract.Publication, error) {
		if !resultMode {
			return owner.PublishDisplaySettings(ctx, request, manager)
		}
		saved, e := owner.PublishDisplaySettingsResult(ctx, request, manager)
		if e != nil {
			return contract.Publication{}, e
		}
		if saved.TenantID != request.TenantID || saved.TableID != request.TableID || saved.Caption != request.Settings.Caption {
			return contract.Publication{}, errors.New("wrong saved settings")
		}
		resultMu.Lock()
		defer resultMu.Unlock()
		if prior, ok := originalResults[request.OperationID]; ok && !reflect.DeepEqual(prior, saved) {
			return contract.Publication{}, errors.New("original settings changed")
		}
		originalResults[request.OperationID] = saved
		return initial, nil
	}
	request := display.PublicationRequest{TenantID: tenantID, TableID: table, ActorID: actor, OperationID: operation, Condition: contract.PublicationCondition{Mode: contract.PublicationExactVersion, Version: initial.Version}, Settings: input}
	if scenario == "service_closed_gate" {
		if _, err := db.ExecContext(ctx, "UPDATE data_model_physical_tables SET write_state='closed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(table)); err != nil {
			t.Fatal(err)
		}
	}
	selected := manager
	var observed atomic.Bool
	if scenario == "service_lost_commit" {
		selected = makeManager(deliveryFaultDB(t, database, &observed))
	}
	var result contract.Publication
	var err error
	if scenario == "service_concurrent" {
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p, e := publish(ctx, request, selected)
				if e == nil && p.Version != initial.Version {
					e = errors.New("changed ingestion token")
				}
				errs <- e
			}()
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
	} else {
		result, err = publish(ctx, request, selected)
	}
	want := 1
	if scenario == "service_missing_queue" || scenario == "service_revision_failure" || scenario == "service_local_denial" || scenario == "service_closed_gate" {
		want = 0
		if err == nil || result.Version != uuid.Nil {
			t.Fatal("unfinished owner returned success", err)
		}
	} else {
		if scenario == "service_lost_commit" {
			var ambiguous *mysqlstore.CommitError
			if !errors.As(err, &ambiguous) || !observed.Load() || result.Version != uuid.Nil {
				t.Fatal("expected ambiguous display commit", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		result, err = publish(ctx, request, manager)
		if err != nil || result.Version != initial.Version || localCalls.Load() != 1 || ids.calls != 2 {
			t.Fatal("owner replay", err, localCalls.Load(), ids.calls)
		}
		revoked.Store(true)
		if _, e := publish(ctx, request, manager); !errors.Is(e, denied) {
			t.Fatal("revoked replay", e)
		}
		revoked.Store(false)
		changed := request
		changed.Settings.Caption = ""
		if _, e := publish(ctx, changed, manager); e == nil {
			t.Fatal("changed replay accepted")
		}
		foreign := request
		foreign.TenantID = uuid.New()
		if _, e := publish(ctx, foreign, manager); e == nil {
			t.Fatal("foreign tenant accepted")
		}
	}
	for _, relation := range []string{"data_model_index_jobs", "data_model_index_deliveries", "data_model_tenant_schema_migrations", "data_model_schema_change_log"} {
		var count int
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+relation+" WHERE tenant_id=?", mysqlstore.UUID(tenantID)).Scan(&count); e != nil || count != baseline[relation]+want {
			t.Fatal("owner atomic effects", relation, count, want, e)
		}
	}
	after, e := settings.Get(ctx, table)
	if e != nil {
		t.Fatal(e)
	}
	if want == 0 {
		if after.Caption != "" || after.Version != *input.Version {
			t.Fatal("display effects survived rollback", after)
		}
		return
	}
	if scenario == "service_restricted" {
		_, e := ownerPool.ExecContext(ctx, "UPDATE data_model_index_delivery_reservations SET tenant_id=? WHERE tenant_id=?", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenantID))
		mysqlError(t, e, 1644) // The sole UPDATE grant cannot change immutable identity.
		schema, _ := mysqlstore.Identifier(database)
		deniedTable := "data_model_tenant_schema_migrations"
		if resultMode {
			deniedTable = "data_model_owner_results"
		}
		if _, e = db.ExecContext(ctx, "REVOKE INSERT ON "+schema+"."+deniedTable+" FROM "+restrictedAccount); e != nil {
			t.Fatal(e)
		}
		next := request
		next.OperationID = uuid.New()
		next.Settings.Version = &after.Version
		next.Settings.Caption = ""
		failed, e := publish(ctx, next, manager)
		if e == nil || failed.Version != uuid.Nil {
			t.Fatal("missing revision privilege accepted", e)
		}
		unchanged, e := settings.Get(ctx, table)
		if e != nil || unchanged.Version != after.Version || unchanged.Caption != after.Caption {
			t.Fatal("grant denial leaked settings", e)
		}
		for _, q := range restrictedGrants {
			if _, e = db.ExecContext(ctx, q); e != nil {
				t.Fatal(e)
			}
		}
	}
	// A new save with the same caption reuses the managed job/delivery, but
	// records a new display revision. Clearing does not enqueue another job.
	for _, caption := range []string{input.Caption, ""} {
		next := request
		next.OperationID = uuid.New()
		next.Settings.Version = &after.Version
		next.Settings.Caption = caption
		if _, e = publish(ctx, next, manager); e != nil {
			t.Fatal(e)
		}
		after, e = settings.Get(ctx, table)
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, relation := range []string{"data_model_index_jobs", "data_model_index_deliveries"} {
		var count int
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+relation+" WHERE tenant_id=?", mysqlstore.UUID(tenantID)).Scan(&count); e != nil || count != baseline[relation]+1 {
			t.Fatal("caption job duplicated", relation, count, e)
		}
	}
	if _, e := publish(ctx, request, manager); e != nil {
		t.Fatal("historical owner replay", e)
	}
	latest, e := settings.Get(ctx, table)
	if e != nil || latest.Caption != "" || latest.Version != after.Version {
		t.Fatal("historical replay changed settings", e)
	}
}
