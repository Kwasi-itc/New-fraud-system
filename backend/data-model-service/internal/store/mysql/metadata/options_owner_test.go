package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func verifyOptionsOwner(t *testing.T, ctx context.Context, db *sql.DB, database string, tenantID, tableID uuid.UUID, creation store.PhysicalPublicationRequest, scenario string) {
	t.Helper()
	schema, _ := mysqlstore.Identifier(database)
	pub, err := store.NewPublicationStore(db, database, 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := pub.RecoverPublication(ctx, tenantID, tableID, creation.Operation)
	if err != nil {
		t.Fatal(err)
	}
	var field mysqlstore.UUID
	if err = db.QueryRowContext(ctx, "SELECT id FROM "+schema+".data_model_fields WHERE tenant_id=? AND table_id=? AND name='object_id'", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&field); err != nil {
		t.Fatal(err)
	}
	var beforeVersion int64
	if err = db.QueryRowContext(ctx, "SELECT display_version FROM "+schema+".data_model_tables WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&beforeVersion); err != nil {
		t.Fatal(err)
	}
	actor := uuid.MustParse(creation.ActorID)
	var revoked atomic.Bool
	var calls atomic.Int32
	denied := errors.New("options denied")
	makeManager := func(pool *sql.DB) *store.MetadataPublicationManager {
		metadata, e := store.NewTransactionManager(pool, database, tenantID, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID) error { calls.Add(1); return nil }, nil)
		if e != nil {
			t.Fatal(e)
		}
		publication, e := store.NewPublicationStore(pool, database, 100, 100, 1<<20)
		if e != nil {
			t.Fatal(e)
		}
		manager, e := store.NewMetadataPublicationManager(metadata, publication, func(_ context.Context, r ports.MetadataPublicationRequest) error {
			if revoked.Load() || r.ActorID != actor || r.TenantID != tenantID || r.TableID != tableID {
				return denied
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
		return manager
	}
	pool := db
	var account string
	if strings.HasSuffix(scenario, "restricted") {
		pool, account = restrictedOwnerPool(t, ctx, db, database, installation.OptionsOwnerPolicy)
	}
	manager := makeManager(pool)
	ids := &creationOwnerIDs{}
	if strings.HasSuffix(scenario, "revision_failure") {
		ids.fail = 3
	}
	owner := service.NewOptionsService(nil, nil, nil, nil, nil, ids, creationOwnerClock{})
	input := service.ReplaceTableOptionsInput{TableID: tableID, DisplayedFields: []uuid.UUID{uuid.UUID(field)}, FieldOrder: []uuid.UUID{uuid.UUID(field)}}
	r := ports.MetadataPublicationRequest{TenantID: tenantID, TableID: tableID, ActorID: actor, OperationID: uuid.New(), Condition: contract.PublicationCondition{Mode: contract.PublicationExactVersion, Version: initial.Version}}
	hash := func(in service.ReplaceTableOptionsInput, request ports.MetadataPublicationRequest) [32]byte {
		h, e := service.PublicationRequestHash(service.PublicationRequestContext{TenantID: request.TenantID, TableID: request.TableID, ActorID: request.ActorID, Condition: request.Condition}, in)
		if e != nil {
			t.Fatal(e)
		}
		return h
	}
	r.RequestHash = hash(input, r)
	if strings.HasSuffix(scenario, "closed_gate") {
		if _, err = db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_tables SET write_state='closed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)); err != nil {
			t.Fatal(err)
		}
	}
	selected := manager
	var observed atomic.Bool
	if strings.HasSuffix(scenario, "lost_commit") {
		selected = makeManager(deliveryFaultDB(t, database, &observed))
	}
	var result datamodel.TableOptions
	if strings.HasSuffix(scenario, "concurrent") {
		db.SetMaxOpenConns(8)
		defer db.SetMaxOpenConns(1)
		start := make(chan struct{})
		var wg sync.WaitGroup
		results := make(chan datamodel.TableOptions, 8)
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				value, e := owner.PublishReplacement(ctx, input, r, selected)
				results <- value
				errs <- e
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		for value := range results {
			if result.ID != uuid.Nil && !reflect.DeepEqual(result, value) {
				t.Fatal("duplicate result changed")
			}
			result = value
		}
	} else {
		result, err = owner.PublishReplacement(ctx, input, r, selected)
	}
	failure := strings.HasSuffix(scenario, "revision_failure") || strings.HasSuffix(scenario, "closed_gate")
	if failure {
		if err == nil || result.ID != uuid.Nil {
			t.Fatal("failed options returned success", err)
		}
	} else {
		if strings.HasSuffix(scenario, "lost_commit") {
			var ambiguous *mysqlstore.CommitError
			if !errors.As(err, &ambiguous) || !observed.Load() || result.ID != uuid.Nil {
				t.Fatal("commit fault", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		result, err = owner.PublishReplacement(ctx, input, r, manager)
		if err != nil || result.ID == uuid.Nil || result.TenantID != tenantID || result.TableID != tableID || len(result.FieldOrder) != 2 || result.FieldOrder[0] != uuid.UUID(field) || calls.Load() != 1 || ids.calls != 3 {
			t.Fatal("original options/repeated effects", result, err, calls.Load(), ids.calls)
		}
	}
	var version int64
	if err = db.QueryRowContext(ctx, "SELECT display_version FROM "+schema+".data_model_tables WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&version); err != nil {
		t.Fatal(err)
	}
	want := beforeVersion + 1
	if failure {
		want = beforeVersion
	}
	if version != want {
		t.Fatal("display version atomicity", version, want)
	}
	for _, name := range []string{"data_model_owner_results", "data_model_publication_operations"} {
		var count int
		if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+name+" WHERE tenant_id=? AND table_id=? AND operation_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(r.OperationID)).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if (failure && count != 0) || (!failure && count != 1) {
			t.Fatal("receipt atomicity", name, count)
		}
	}
	if failure {
		var count int
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_table_options WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&count); e != nil || count != 0 {
			t.Fatal("failed options persisted", e, count)
		}
		return
	}
	legacy := r
	legacy.OperationID = uuid.New()
	if _, e := manager.Publish(ctx, legacy, func(ports.MutationStore) error { return nil }); e != nil {
		t.Fatal(e)
	}
	if v, e := owner.PublishReplacement(ctx, input, legacy, manager); !errors.Is(e, store.ErrOwnerResultUnavailable) || v.ID != uuid.Nil {
		t.Fatal("legacy result reconstructed", e)
	}
	for _, badFields := range [][]uuid.UUID{{uuid.Nil}, {uuid.UUID(field), uuid.UUID(field)}, make([]uuid.UUID, 13)} {
		badInput := input
		badInput.DisplayedFields = badFields
		badRequest := r
		badRequest.OperationID = uuid.New()
		badRequest.RequestHash = hash(badInput, badRequest)
		before := calls.Load()
		if v, e := owner.PublishReplacement(ctx, badInput, badRequest, manager); e == nil || v.ID != uuid.Nil || calls.Load() != before {
			t.Fatal("invalid lists reached mutation", e)
		}
	}
	// A different request must not reuse the receipt, even if presentation edits
	// leave the ingestion version unchanged.
	changed := input
	changed.DisplayedFields = nil
	altered := r
	altered.RequestHash = hash(changed, altered)
	if _, e := owner.PublishReplacement(ctx, changed, altered, manager); !errors.Is(e, store.ErrPublicationOperationConflict) {
		t.Fatal("changed command replay", e)
	}
	foreign := input
	foreign.DisplayedFields = []uuid.UUID{uuid.New()}
	bad := r
	bad.OperationID = uuid.New()
	bad.RequestHash = hash(foreign, bad)
	if v, e := owner.PublishReplacement(ctx, foreign, bad, manager); e == nil || v.ID != uuid.Nil {
		t.Fatal("foreign field accepted", e)
	}
	// Save a newer state, then close the physical barrier: historical replay must
	// retain the old sorted response and must not reopen or overwrite anything.
	newRequest := altered
	newRequest.OperationID = uuid.New()
	newer, e := owner.PublishReplacement(ctx, changed, newRequest, manager)
	if e != nil || newer.ID != result.ID || len(newer.DisplayedFields) != 0 {
		t.Fatal("replacement identity", e)
	}
	if account != "" {
		if _, e = db.ExecContext(ctx, "REVOKE INSERT ON "+schema+".data_model_owner_results FROM "+account); e != nil {
			t.Fatal(e)
		}
		deniedRequest := r
		deniedRequest.OperationID = uuid.New()
		if v, e := owner.PublishReplacement(ctx, input, deniedRequest, manager); e == nil || v.ID != uuid.Nil {
			t.Fatal("missing result grant accepted")
		}
		var after int64
		if e = db.QueryRowContext(ctx, "SELECT display_version FROM "+schema+".data_model_tables WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&after); e != nil || after != beforeVersion+2 {
			t.Fatal("grant failure leaked options", e, after)
		}
	}
	if _, e = db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_tables SET write_state='closed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)); e != nil {
		t.Fatal(e)
	}
	replayed, e := owner.PublishReplacement(ctx, input, r, manager)
	if e != nil || !reflect.DeepEqual(result, replayed) {
		t.Fatal("historical options changed", e)
	}
	revoked.Store(true)
	if v, e := owner.PublishReplacement(ctx, input, r, manager); !errors.Is(e, denied) || v.ID != uuid.Nil {
		t.Fatal("revoked replay", e)
	}
}
