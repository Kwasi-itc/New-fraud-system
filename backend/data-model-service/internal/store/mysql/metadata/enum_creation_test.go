package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestEnumCreationPublicationRecovery(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	guard := installation.Definer{User: "ec_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24], Host: "%"}
	account, e := guard.SQLAccount()
	if e != nil {
		t.Fatal(e)
	}
	create, e := installation.CreateDefiner(guard)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, create); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := db.Exec("DROP USER " + account); e != nil {
			t.Error(e)
		}
	})
	upgrades, e := installation.OwnerResultUpgradeStatements(database, guard)
	if e != nil {
		t.Fatal(e)
	}
	for _, step := range upgrades {
		if _, e = db.ExecContext(ctx, step.SQL); e != nil {
			t.Fatal(e)
		}
	}
	grants, e := installation.GrantStatements(database, installation.OwnerResultDefinerPolicy(guard))
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range grants {
		if _, e = db.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	tid, table, field := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, e := store.NewMetadataRepositories(tx, database, tid, 1000, 4<<20)
		if e != nil {
			return e
		}
		if e = r.Tenants.Create(ctx, tenant.New(tid, now, "enum", nil)); e != nil {
			return e
		}
		if e = r.Tenants.UpdateStatus(ctx, tid, tenant.StatusActive); e != nil {
			return e
		}
		if e = r.Tables.Create(ctx, datamodel.Table{ID: table, TenantID: tid, Name: "records", CreatedAt: now, UpdatedAt: now}); e != nil {
			return e
		}
		if e = r.Fields.Create(ctx, datamodel.Field{ID: field, TenantID: tid, TableID: table, Name: "state", DataType: datamodel.DataTypeString, IsEnum: true, CreatedAt: now, UpdatedAt: now}); e != nil {
			return e
		}
		registry, e := store.NewPhysicalRegistry(tx, database, tid, 1000)
		if e != nil {
			return e
		}
		return registry.RegisterTable(ctx, table)
	})
	if err != nil {
		t.Fatal(err)
	}
	publication, err := store.NewPublicationStore(db, database, 1000, 1000, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := publication.MutateAndPublish(ctx, tid, table, func(*sql.Tx) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE data_model_physical_tables SET published_version=?,write_state='open' WHERE table_id=?", mysqlstore.UUID(initial.Version), mysqlstore.UUID(table)); err != nil {
		t.Fatal(err)
	}
	makeManager := func(pool *sql.DB) *store.MetadataPublicationManager {
		t.Helper()
		m, e := store.NewTransactionManager(pool, database, tid, 1000, 4<<20, func(context.Context, *sql.Tx, uuid.UUID) error { return nil }, nil)
		if e != nil {
			t.Fatal(e)
		}
		p, e := store.NewPublicationStore(pool, database, 1000, 1000, 4<<20)
		if e != nil {
			t.Fatal(e)
		}
		manager, e := store.NewMetadataPublicationManager(m, p, func(context.Context, ports.MetadataPublicationRequest) error { return nil })
		if e != nil {
			t.Fatal(e)
		}
		return manager
	}
	owner := service.NewFieldEnumValueService(nil, nil, nil, nil, &creationOwnerIDs{}, creationOwnerClock{})
	manager := makeManager(db)
	for _, scenario := range []string{"rollback", "lost_commit"} {
		t.Run(scenario, func(t *testing.T) {
			var current mysqlstore.UUID
			if e := db.QueryRowContext(ctx, "SELECT published_version FROM data_model_physical_tables WHERE table_id=?", mysqlstore.UUID(table)).Scan(&current); e != nil {
				t.Fatal(e)
			}
			id := uuid.New()
			request := ports.MetadataPublicationRequest{TenantID: tid, TableID: table, ActorID: uuid.New(), OperationID: uuid.New(), RequestHash: [32]byte{1}, Condition: store.PublicationCondition{Mode: store.PublicationExactVersion, Version: uuid.UUID(current)}}
			fail := scenario == "rollback"
			calls := 0
			effects := func(m ports.MutationStore) (datamodel.FieldEnumValue, error) {
				calls++
				v, e := owner.ApplyCatalogCreation(ctx, m, service.CreateFieldEnumValueInput{FieldID: field, Value: scenario, Label: scenario}, id, tid, table)
				if e == nil && fail {
					return v, errors.New("injected owner rollback")
				}
				return v, e
			}
			selected := manager
			var observed atomic.Bool
			if scenario == "lost_commit" {
				selected = makeManager(deliveryFaultDB(t, database, &observed))
			}
			if _, e := selected.PublishEnumCreation(ctx, request, id, field, effects); e == nil {
				t.Fatal("injected failure missing")
			}
			var count int
			if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_field_enum_values WHERE id=?", mysqlstore.UUID(id)).Scan(&count); e != nil {
				t.Fatal(e)
			}
			if scenario == "rollback" && count != 0 {
				t.Fatal("enum leaked on rollback")
			}
			if scenario == "lost_commit" && (!observed.Load() || count != 1) {
				t.Fatal("lost commit not durable")
			}
			fail = false
			result, e := manager.PublishEnumCreation(ctx, request, id, field, effects)
			if e != nil || result.ID != id {
				t.Fatal(result, e)
			}
			before := calls
			if _, e = manager.PublishEnumCreation(ctx, request, id, field, effects); e != nil || calls != before {
				t.Fatal("duplicate effects", e)
			}
			foreign := request
			foreign.TenantID = uuid.New()
			if _, e = manager.PublishEnumCreation(ctx, foreign, id, field, effects); e == nil {
				t.Fatal("tenant isolation bypass")
			}
		})
	}
}
