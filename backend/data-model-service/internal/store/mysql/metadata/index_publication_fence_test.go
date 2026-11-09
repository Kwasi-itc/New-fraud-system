package metadata_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestPublishedNativeIndexCompletion(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	at := time.Now().UTC().Truncate(time.Microsecond)
	for _, scenario := range []string{"valid", "closed", "wrong_version", "missing_index", "wrong_column", "invisible", "unique", "audit_failure", "foreign_table", "caller_cannot_replace_columns", "expired", "publication_changes_while_waiting"} {
		t.Run(scenario, func(t *testing.T) {
			tenantID, tableID, fieldID, jobID, version := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			queue, err := store.NewIndexDeliveryStore(db, database, "fence_"+scenario, 3, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 1<<20)
				if err != nil {
					return err
				}
				if err := r.Tenants.Create(ctx, tenant.New(tenantID, at, "fence", nil)); err != nil {
					return err
				}
				if err := r.Tables.Create(ctx, datamodel.Table{ID: tableID, TenantID: tenantID, Name: "events", CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
				if err := r.Fields.Create(ctx, datamodel.Field{ID: fieldID, TenantID: tenantID, TableID: tableID, Name: "amount", DataType: datamodel.DataTypeInt, CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
				registry, err := store.NewPhysicalRegistry(tx, database, tenantID, 100)
				if err != nil {
					return err
				}
				if err := registry.RegisterTable(ctx, tableID); err != nil {
					return err
				}
				if err := registry.RegisterFields(ctx, tableID, []uuid.UUID{fieldID}); err != nil {
					return err
				}
				if err := r.Jobs.Create(ctx, datamodel.IndexJob{ID: jobID, TenantID: tenantID, TableID: &tableID, TableName: "events", IndexType: "search", Columns: []string{"amount"}, Status: "pending", RequestedAt: at}); err != nil {
					return err
				}
				_, err = queue.EnqueueTx(ctx, tx, tenantID, jobID, nil)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			var physical, column string
			if err := db.QueryRowContext(ctx, "SELECT physical_name FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&physical); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, "SELECT physical_name FROM data_model_physical_fields WHERE tenant_id=? AND table_id=? AND field_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(fieldID)).Scan(&column); err != nil {
				t.Fatal(err)
			}
			// Privileged disposable setup isolates the completion gate. Actual DDL
			// intent execution/publication wiring remains a separate acceptance case.
			quoted, _ := mysqlstore.Identifier(physical)
			quotedColumn, _ := mysqlstore.Identifier(column)
			index := "record_index_" + strings.ReplaceAll(jobID.String(), "-", "")
			definition := "CREATE TABLE " + quoted + " (id BINARY(16) PRIMARY KEY," + quotedColumn + " BIGINT NOT NULL"
			if scenario != "missing_index" {
				modifier, selected, visibility := "", quotedColumn, ""
				if scenario == "unique" {
					modifier = "UNIQUE "
				}
				if scenario == "wrong_column" || scenario == "caller_cannot_replace_columns" {
					selected = "id"
				}
				if scenario == "invisible" {
					visibility = " INVISIBLE"
				}
				definition += "," + modifier + "INDEX `" + index + "` (" + selected + ")" + visibility
			}
			if _, err := db.ExecContext(ctx, definition+") ENGINE=InnoDB"); err != nil {
				t.Fatal(err)
			}
			gate := "open"
			if scenario == "closed" {
				gate = "closed"
			}
			if _, err := db.ExecContext(ctx, "UPDATE data_model_physical_tables SET published_version=?,write_state=? WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(version), gate, mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)); err != nil {
				t.Fatal(err)
			}
			lease, err := queue.Claim(ctx)
			if err != nil || lease == nil {
				t.Fatal(lease, err)
			}
			execution, err := store.NewLeasedIndexJobExecutionStore(queue, *lease, 1<<20, 0)
			if err != nil {
				t.Fatal(err)
			}
			job, err := execution.StartAttempt(ctx, jobID, at)
			if err != nil {
				t.Fatal(err)
			}
			boundTable, boundVersion := tableID, version
			if scenario == "wrong_version" {
				boundVersion = uuid.New()
			}
			if scenario == "foreign_table" {
				boundTable = uuid.New()
			}
			fenced, err := execution.WithPublishedNativeIndex(boundTable, boundVersion)
			if err != nil {
				t.Fatal(err)
			}
			outcome := ports.IndexJobOutcome{Job: job, Change: datamodel.SchemaChange{ID: uuid.New(), TenantID: tenantID, Operation: "apply_index_job", ResourceType: "index_job", ResourceID: jobID, Status: "applied", Details: []byte(`{}`), CreatedAt: at}}
			if scenario == "caller_cannot_replace_columns" {
				outcome.Job.Columns = []string{"id"}
			}
			if scenario == "audit_failure" {
				if _, err := db.ExecContext(ctx, "CREATE TRIGGER fixture_fail_index_audit BEFORE INSERT ON data_model_schema_change_log FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='audit failure'"); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := db.ExecContext(ctx, "DROP TRIGGER fixture_fail_index_audit"); err != nil {
						t.Error(err)
					}
				}()
			}
			if scenario == "expired" {
				if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(lease.ID)); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "publication_changes_while_waiting" {
				publisher, beginErr := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
				if beginErr != nil {
					t.Fatal(beginErr)
				}
				defer publisher.Rollback()
				var held mysqlstore.UUID
				if lockErr := publisher.QueryRowContext(ctx, "SELECT table_id FROM data_model_physical_tables WHERE tenant_id=? AND table_id=? FOR UPDATE", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&held); lockErr != nil {
					t.Fatal(lockErr)
				}
				finished := make(chan error, 1)
				go func() { finished <- fenced.CompleteAttempt(ctx, outcome) }()
				// Observe an actual registry lock wait, not a timing assumption.
				deadline := time.Now().Add(10 * time.Second)
				waiting := false
				for time.Now().Before(deadline) {
					var n int
					if queryErr := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='data_model_physical_tables'", database).Scan(&n); queryErr != nil {
						publisher.Rollback()
						<-finished
						t.Fatal(queryErr)
					}
					if n > 0 {
						waiting = true
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !waiting {
					publisher.Rollback()
					<-finished
					t.Fatal("completion never waited for registry")
				}
				// A completion that locked the job first would invert this order.
				if lockErr := publisher.QueryRowContext(ctx, "SELECT id FROM data_model_index_jobs WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(tenantID), mysqlstore.UUID(jobID)).Scan(&held); lockErr != nil {
					publisher.Rollback()
					<-finished
					t.Fatal(lockErr)
				}
				if _, changeErr := publisher.ExecContext(ctx, "UPDATE data_model_physical_tables SET published_version=? WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)); changeErr != nil {
					publisher.Rollback()
					<-finished
					t.Fatal(changeErr)
				}
				if commitErr := publisher.Commit(); commitErr != nil {
					t.Fatal(commitErr)
				}
				err = <-finished
			} else {
				err = fenced.CompleteAttempt(ctx, outcome)
			}
			var jobStatus, deliveryStatus string
			var audits int
			if readErr := db.QueryRowContext(ctx, "SELECT status FROM data_model_index_jobs WHERE id=?", mysqlstore.UUID(jobID)).Scan(&jobStatus); readErr != nil {
				t.Fatal(readErr)
			}
			if readErr := db.QueryRowContext(ctx, "SELECT state FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(lease.ID)).Scan(&deliveryStatus); readErr != nil {
				t.Fatal(readErr)
			}
			if readErr := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE id=?", mysqlstore.UUID(outcome.Change.ID)).Scan(&audits); readErr != nil {
				t.Fatal(readErr)
			}
			if scenario == "valid" {
				if err != nil || jobStatus != "applied" || deliveryStatus != "completed" || audits != 1 {
					t.Fatal(jobStatus, deliveryStatus, audits, err)
				}
			} else if err == nil || jobStatus != "running" || deliveryStatus != "running" || audits != 0 {
				t.Fatal("failed guard leaked completion", jobStatus, deliveryStatus, audits, err)
			}
		})
	}
}
