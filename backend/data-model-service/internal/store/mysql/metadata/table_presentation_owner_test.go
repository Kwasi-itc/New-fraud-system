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
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func verifyTablePresentationOwner(t *testing.T, ctx context.Context, db *sql.DB, database string, tenantID, tableID uuid.UUID, creation store.PhysicalPublicationRequest, scenario string) {
	t.Helper()
	resultMode := strings.Contains(scenario, "_result_")
	parallelResults := resultMode && strings.HasSuffix(scenario, "concurrent")
	if parallelResults {
		db.SetMaxOpenConns(8)
		defer db.SetMaxOpenConns(1)
	}
	var initialAuthorizations atomic.Int32
	startConcurrent := make(chan struct{})
	schema, _ := mysqlstore.Identifier(database)
	pub, err := store.NewPublicationStore(db, database, 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := pub.RecoverPublication(ctx, tenantID, tableID, creation.Operation)
	if err != nil {
		t.Fatal(err)
	}
	var localCalls atomic.Int32
	var revoked atomic.Bool
	denied := errors.New("presentation denied")
	actor := uuid.MustParse(creation.ActorID)
	makeManager := func(pool *sql.DB) *store.MetadataPublicationManager {
		metadata, e := store.NewTransactionManager(pool, database, tenantID, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID) error { localCalls.Add(1); return nil }, nil)
		if e != nil {
			t.Fatal(e)
		}
		publication, e := store.NewPublicationStore(pool, database, 100, 100, 1<<20)
		if e != nil {
			t.Fatal(e)
		}
		manager, e := store.NewMetadataPublicationManager(metadata, publication, func(ctx context.Context, r ports.MetadataPublicationRequest) error {
			if revoked.Load() || r.TenantID != tenantID || r.TableID != tableID || r.ActorID != actor {
				return denied
			}
			if parallelResults {
				n := initialAuthorizations.Add(1)
				if n <= 8 {
					if n == 8 {
						close(startConcurrent)
					}
					select {
					case <-startConcurrent:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
			return db.PingContext(ctx)
		})
		if e != nil {
			t.Fatal(e)
		}
		return manager
	}
	ownerPool := db
	var restrictedAccount string
	if strings.HasSuffix(scenario, "restricted") {
		ownerPool, restrictedAccount = restrictedPresentationPool(t, ctx, db, database)
	}
	manager := makeManager(ownerPool)
	ids := &creationOwnerIDs{}
	if strings.HasSuffix(scenario, "revision_failure") {
		ids.fail = 2
	}
	owner := service.NewTableService(nil, nil, nil, nil, nil, nil, nil, nil, ids, creationOwnerClock{})
	fieldMode := strings.HasPrefix(scenario, "service_allocated_field_presentation_")
	var fieldID mysqlstore.UUID
	if fieldMode {
		if e := db.QueryRowContext(ctx, "SELECT id FROM "+schema+".data_model_fields WHERE tenant_id=? AND table_id=? AND name='object_id'", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&fieldID); e != nil {
			t.Fatal(e)
		}
	}
	fieldOwner := service.NewFieldService(nil, nil, nil, nil, nil, nil, nil, nil, nil, ids, creationOwnerClock{})
	fieldInput := func(in service.UpdateTableInput) service.UpdateFieldInput {
		return service.UpdateFieldInput{FieldID: uuid.UUID(fieldID), Alias: in.Alias, Description: in.Description}
	}
	requestHash := func(scope service.PublicationRequestContext, in service.UpdateTableInput) ([32]byte, error) {
		if fieldMode {
			return service.PublicationRequestHash(scope, fieldInput(in))
		}
		return service.PublicationRequestHash(scope, in)
	}
	var resultMu sync.Mutex
	originalResults := map[uuid.UUID]any{}
	publish := func(ctx context.Context, in service.UpdateTableInput, r ports.MetadataPublicationRequest, m ports.MetadataPublicationManager) (contract.Publication, error) {
		if resultMode {
			typed, ok := m.(ports.MetadataResultPublicationManager)
			if !ok {
				return contract.Publication{}, errors.New("typed owner manager required")
			}
			var saved any
			var e error
			if fieldMode {
				v, err := fieldOwner.PublishPresentationResult(ctx, fieldInput(in), r, typed)
				saved, e = v, err
				if err == nil && (v.ID != uuid.UUID(fieldID) || v.TenantID != tenantID || v.TableID != tableID || v.Alias != *in.Alias) {
					return contract.Publication{}, errors.New("wrong original Field")
				}
			} else {
				v, err := owner.PublishPresentationResult(ctx, in, r, typed)
				saved, e = v, err
				if err == nil && (v.ID != tableID || v.TenantID != tenantID || v.Alias != *in.Alias) {
					return contract.Publication{}, errors.New("wrong original Table")
				}
			}
			if e != nil {
				return contract.Publication{}, e
			}
			resultMu.Lock()
			defer resultMu.Unlock()
			if prior, ok := originalResults[r.OperationID]; ok && !reflect.DeepEqual(prior, saved) {
				return contract.Publication{}, errors.New("original owner response changed")
			}
			originalResults[r.OperationID] = saved
			return initial, nil
		}
		if fieldMode {
			return fieldOwner.PublishPresentationUpdate(ctx, fieldInput(in), r, m)
		}
		return owner.PublishPresentationUpdate(ctx, in, r, m)
	}
	alias, description, semantic := "updated alias", "updated description", "customer"
	input := service.UpdateTableInput{TableID: tableID, Alias: &alias, Description: &description, SemanticType: &semantic}
	r := ports.MetadataPublicationRequest{TenantID: tenantID, TableID: tableID, ActorID: actor, OperationID: uuid.New(), Condition: contract.PublicationCondition{Mode: contract.PublicationExactVersion, Version: initial.Version}}
	r.RequestHash, err = requestHash(service.PublicationRequestContext{TenantID: tenantID, TableID: tableID, ActorID: actor, Condition: r.Condition}, input)
	if err != nil {
		t.Fatal(err)
	}
	if fieldMode {
		// Even unchanged enforcement flags must use the physical owner protocol.
		for _, kind := range []string{"nullable", "enum", "unique", "changed_identity"} {
			invalid := fieldInput(input)
			flag := false
			switch kind {
			case "nullable":
				invalid.Nullable = &flag
			case "enum":
				invalid.IsEnum = &flag
			case "unique":
				invalid.IsUnique = &flag
			case "changed_identity":
				invalid.FieldID = uuid.New()
			}
			badRequest := r
			if kind != "changed_identity" {
				badRequest.RequestHash, err = service.PublicationRequestHash(service.PublicationRequestContext{TenantID: tenantID, TableID: tableID, ActorID: actor, Condition: r.Condition}, invalid)
				if err != nil {
					t.Fatal(err)
				}
			}
			if p, e := fieldOwner.PublishPresentationUpdate(ctx, invalid, badRequest, manager); e == nil || p.Version != uuid.Nil {
				t.Fatal("invalid field presentation accepted", kind, e)
			}
		}
		if localCalls.Load() != 0 || ids.calls != 0 {
			t.Fatal("invalid request reached owner effects")
		}
	}
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
	var result contract.Publication
	if strings.HasSuffix(scenario, "concurrent") {
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p, e := publish(ctx, input, r, selected)
				if e == nil && p.Version != initial.Version {
					e = errors.New("presentation changed token")
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
	} else if strings.HasSuffix(scenario, "caption_change") || strings.HasSuffix(scenario, "archive") {
		result, err = manager.Publish(ctx, r, func(s ports.MutationStore) error {
			table, e := s.Tables().GetByID(ctx, tableID)
			if e != nil {
				return e
			}
			if strings.HasSuffix(scenario, "archive") {
				table.Archived = true
			} else {
				table.CaptionField = "object_id"
			}
			return s.Tables().Update(ctx, table)
		})
	} else if strings.HasSuffix(scenario, "semantic_change") {
		result, err = manager.Publish(ctx, r, func(s ports.MutationStore) error {
			fields, e := s.Fields().ListByTable(ctx, tableID)
			if e != nil {
				return e
			}
			f := fields[0]
			f.Nullable = !f.Nullable
			return s.Fields().Update(ctx, f)
		})
	} else {
		result, err = publish(ctx, input, r, selected)
	}
	want := 1
	if strings.HasSuffix(scenario, "revision_failure") || strings.HasSuffix(scenario, "closed_gate") || strings.HasSuffix(scenario, "semantic_change") || strings.HasSuffix(scenario, "caption_change") || strings.HasSuffix(scenario, "archive") {
		want = 0
		if err == nil || result.Version != uuid.Nil {
			t.Fatal("unsafe presentation success", err)
		}
	} else {
		if strings.HasSuffix(scenario, "lost_commit") {
			var ambiguous *mysqlstore.CommitError
			if !errors.As(err, &ambiguous) || !observed.Load() || result.Version != uuid.Nil {
				t.Fatal("lost owner commit", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		result, err = publish(ctx, input, r, manager)
		if err != nil || result.Version != initial.Version || localCalls.Load() != 1 || ids.calls != 2 {
			t.Fatal("owner replay repeated effects", err, localCalls.Load(), ids.calls)
		}
		revoked.Store(true)
		if _, e := publish(ctx, input, r, manager); !errors.Is(e, denied) {
			t.Fatal("revoked replay", e)
		}
		revoked.Store(false)
		changed := input
		other := "different"
		changed.Alias = &other
		if _, e := publish(ctx, changed, r, manager); e == nil {
			t.Fatal("changed request accepted")
		}
	}
	for _, relation := range []string{"data_model_schema_change_log", "data_model_tenant_schema_migrations"} {
		var n int
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+relation+" WHERE tenant_id=?", mysqlstore.UUID(tenantID)).Scan(&n); e != nil || n != 1+want {
			t.Fatal("non-atomic owner effects", relation, n, e)
		}
	}
	relation, entityID := "data_model_tables", tableID
	if fieldMode {
		relation, entityID = "data_model_fields", uuid.UUID(fieldID)
	}
	var gotAlias string
	var token mysqlstore.UUID
	var receipts, nullable int
	if e := db.QueryRowContext(ctx, "SELECT alias FROM "+schema+"."+relation+" WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(entityID)).Scan(&gotAlias); e != nil {
		t.Fatal(e)
	}
	if (want == 1 && gotAlias != alias) || (want == 0 && gotAlias != "") {
		t.Fatal("presentation rollback", gotAlias)
	}
	if e := db.QueryRowContext(ctx, "SELECT version FROM "+schema+".data_model_ingestion_contracts WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&token); e != nil || uuid.UUID(token) != initial.Version {
		t.Fatal("semantic token changed", e)
	}
	if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_fields WHERE tenant_id=? AND table_id=? AND nullable", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&nullable); e != nil || nullable != 0 {
		t.Fatal("semantic effects leaked", e)
	}
	if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_publication_operations WHERE tenant_id=? AND table_id=? AND operation_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(r.OperationID)).Scan(&receipts); e != nil || receipts != want {
		t.Fatal("receipt rollback", e, receipts)
	}
	if want == 0 {
		return
	}
	if resultMode {
		var count int
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_owner_results WHERE tenant_id=? AND table_id=? AND operation_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(r.OperationID)).Scan(&count); e != nil || count != 1 {
			t.Fatal("missing original result", count, e)
		}
		if fieldMode {
			if _, e := manager.PublishField(ctx, r, uuid.New(), func(ports.MutationStore) error { t.Fatal("replay ran effects"); return nil }); !errors.Is(e, store.ErrPublicationOperationConflict) {
				t.Fatal("substituted field result", e)
			}
			if _, e := manager.PublishTable(ctx, r, func(ports.MutationStore) error { t.Fatal("replay ran effects"); return nil }); !errors.Is(e, store.ErrPublicationOperationConflict) {
				t.Fatal("substituted result kind", e)
			}
		}
	}
	if resultMode {
		legacy := r
		legacy.OperationID = uuid.New()
		if _, e := manager.Publish(ctx, legacy, func(ports.MutationStore) error { return nil }); e != nil {
			t.Fatal("legacy publication", e)
		}
		if _, e := publish(ctx, input, legacy, manager); !errors.Is(e, store.ErrOwnerResultUnavailable) {
			t.Fatal("legacy current-state reconstruction", e)
		}
	}
	if restrictedAccount != "" {
		if _, e := db.ExecContext(ctx, "REVOKE INSERT ON "+schema+".data_model_owner_results FROM "+restrictedAccount); e != nil {
			t.Fatal(e)
		}
		failedRequest := r
		failedRequest.OperationID = uuid.New()
		if _, e := publish(ctx, input, failedRequest, manager); e == nil {
			t.Fatal("missing result INSERT accepted")
		}
		for _, relation := range []string{"data_model_publication_operations", "data_model_owner_results"} {
			var n int
			if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+relation+" WHERE tenant_id=? AND table_id=? AND operation_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(failedRequest.OperationID)).Scan(&n); e != nil || n != 0 {
				t.Fatal("result denial leaked receipt", e, n)
			}
		}
		for _, relation := range []string{"data_model_schema_change_log", "data_model_tenant_schema_migrations"} {
			var n int
			if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+relation+" WHERE tenant_id=?", mysqlstore.UUID(tenantID)).Scan(&n); e != nil || n != 2 {
				t.Fatal("result denial leaked owner effects", e, n)
			}
		}
		if _, e := db.ExecContext(ctx, "GRANT INSERT ON "+schema+".data_model_owner_results TO "+restrictedAccount); e != nil {
			t.Fatal(e)
		}
	}
	// A later authorized operation can retain the same semantic token. Historical
	// recovery must not restore old presentation data or reopen a later barrier.
	next := input
	later := "later alias"
	next.Alias = &later
	nextRequest := r
	nextRequest.OperationID = uuid.New()
	nextRequest.RequestHash, err = requestHash(service.PublicationRequestContext{TenantID: tenantID, TableID: tableID, ActorID: actor, Condition: r.Condition}, next)
	if err != nil {
		t.Fatal(err)
	}
	if _, e := publish(ctx, next, nextRequest, manager); e != nil {
		t.Fatal(e)
	}
	if _, e := db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_tables SET write_state='closed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)); e != nil {
		t.Fatal(e)
	}
	if _, e := publish(ctx, input, r, manager); e != nil {
		t.Fatal("historical recovery", e)
	}
	var gate string
	joinID := "t.id"
	if fieldMode {
		joinID = "t.table_id"
	}
	if e := db.QueryRowContext(ctx, "SELECT t.alias,p.write_state FROM "+schema+"."+relation+" t JOIN "+schema+".data_model_physical_tables p ON p.tenant_id=t.tenant_id AND p.table_id="+joinID+" WHERE t.tenant_id=? AND t.id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(entityID)).Scan(&gotAlias, &gate); e != nil || gotAlias != later || gate != "closed" {
		t.Fatal("historical replay changed state", e, gotAlias, gate)
	}
}
