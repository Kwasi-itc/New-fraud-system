package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestCreationPreparationTransaction(t *testing.T) {
	db, database := metadataDB(t)
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	scope := uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, scope, 100, 1<<20)
		if err != nil {
			return err
		}
		return r.Tenants.Create(ctx, tenant.New(scope, at, "preparation", nil))
	}); err != nil {
		t.Fatal(err)
	}
	intents, err := store.NewDDLIntentStore(db, database, scope, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"commit_replay", "owner_rollback", "invalid_intent", "foreign_scope", "finished_transaction"} {
		t.Run(scenario, func(t *testing.T) {
			table, field := uuid.New(), uuid.New()
			request := store.DDLIntent{TableID: table, OperationID: uuid.New(), RequestHash: sha256.Sum256([]byte(scenario)), ActorKind: "api", ActorID: uuid.NewString(), Shape: []byte(`{"version":1,"action":"fixture"}`)}
			injected := errors.New("owner failure")
			var escaped *sql.Tx
			err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				escaped = tx
				r, err := store.NewMetadataRepositories(tx, database, scope, 100, 1<<20)
				if err != nil {
					return err
				}
				if err := r.Tables.Create(ctx, datamodel.Table{ID: table, TenantID: scope, Name: table.String(), CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
				if err := r.Fields.Create(ctx, datamodel.Field{ID: field, TenantID: scope, TableID: table, Name: "object_id", DataType: datamodel.DataTypeString, IsUnique: true, CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
				registry, err := store.NewPhysicalRegistry(tx, database, scope, 100)
				if err != nil {
					return err
				}
				if err := registry.RegisterTable(ctx, table); err != nil {
					return err
				}
				if err := registry.RegisterFields(ctx, table, []uuid.UUID{field}); err != nil {
					return err
				}
				selected := intents
				if scenario == "foreign_scope" {
					selected, err = store.NewDDLIntentStore(db, database, uuid.New(), 1<<20)
					if err != nil {
						return err
					}
				}
				if scenario == "invalid_intent" {
					request.RequestHash = [32]byte{}
				}
				result, err := selected.PrepareTx(ctx, tx, request)
				if err != nil {
					if result != (store.DDLIntentResult{}) {
						t.Fatal("partial result on failure", result)
					}
					return err
				}
				if result.OperationID != request.OperationID || result.Replayed || result.State != "prepared" {
					t.Fatal(result)
				}
				if scenario == "owner_rollback" {
					return injected
				}
				return nil
			})
			success := scenario == "commit_replay" || scenario == "finished_transaction"
			if success && err != nil {
				t.Fatal(err)
			}
			if !success && err == nil {
				t.Fatal("failure committed")
			}
			if scenario == "owner_rollback" && !errors.Is(err, injected) {
				t.Fatal(err)
			}
			for _, row := range []struct {
				table, column string
				id            uuid.UUID
			}{
				{"data_model_tables", "id", table},
				{"data_model_fields", "id", field},
				{"data_model_physical_tables", "table_id", table},
				{"data_model_physical_fields", "field_id", field},
				{"data_model_schema_change_intents", "operation_id", request.OperationID},
			} {
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+database+"`."+row.table+" WHERE tenant_id=? AND "+row.column+"=?", mysqlstore.UUID(scope), mysqlstore.UUID(row.id)).Scan(&count); err != nil {
					t.Fatal(err)
				}
				want := 0
				if success {
					want = 1
				}
				if count != want {
					t.Fatalf("%s count=%d want=%d", row.table, count, want)
				}
			}
			if success {
				replay, err := intents.Prepare(ctx, request)
				if err != nil || !replay.Replayed {
					t.Fatal(replay, err)
				}
				var epoch int64
				var gate string
				if err := db.QueryRowContext(ctx, "SELECT epoch,write_state FROM `"+database+"`.data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(scope), mysqlstore.UUID(table)).Scan(&epoch, &gate); err != nil || epoch != 1 || gate != "closed" {
					t.Fatal(epoch, gate, err)
				}
				if result, err := intents.PrepareTx(ctx, escaped, request); !errors.Is(err, sql.ErrTxDone) || result != (store.DDLIntentResult{}) {
					t.Fatal("escaped transaction", result, err)
				}
			}
		})
	}
	if result, err := intents.PrepareTx(ctx, nil, store.DDLIntent{}); err == nil || result != (store.DDLIntentResult{}) {
		t.Fatal(result, err)
	}
}
