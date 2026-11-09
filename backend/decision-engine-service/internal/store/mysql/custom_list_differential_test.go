package mysql

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestCustomListPostgresMySQLLookupParity(t *testing.T) {
	ctx := context.Background()
	pg := historyParityPostgres(t)
	source := postgres.NewCustomListRepository(pg)
	f := scenarioFoundationDB(t, true, true, true, true, true, true)
	tenant := uuid.New()
	target, err := NewCustomListStore(f.db, f.database, tenant, 20, 4096, allowAuthoringFixture)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	names := []string{"Watch list", uuid.NewString()}
	lists := make([]platform.CustomList, 0, 2)
	for _, name := range names {
		list := platform.CustomList{ID: uuid.NewString(), TenantID: tenant.String(), Name: name, Kind: "generic_text", CreatedAt: at, UpdatedAt: at}
		entry := platform.CustomListEntry{ID: uuid.NewString(), TenantID: tenant.String(), ListID: &list.ID, ListName: name, Value: "Exact Value ", CreatedAt: at}
		if _, err := source.CreateList(ctx, list); err != nil {
			t.Fatal(err)
		}
		if _, err := source.Create(ctx, entry); err != nil {
			t.Fatal(err)
		}
		if err := target.Mutate(ctx, func(r CustomListRepository) error {
			if _, err := r.CreateList(ctx, list); err != nil {
				return err
			}
			_, err := r.Create(ctx, entry)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		lists = append(lists, list)
	}
	for _, ref := range []string{lists[0].ID, names[0], names[1], uuid.NewString()} {
		t.Run(ref, func(t *testing.T) {
			node := domainast.Node{Function: "CustomListAccess", NamedChildren: map[string]domainast.Node{"customListId": {Constant: ref}}}
			left, err := ast_eval.EvaluateNode(ctx, node, ast_eval.Runtime{TenantID: tenant.String(), CustomListRepo: source})
			if err != nil {
				t.Fatal(err)
			}
			if err := target.ReadSnapshot(ctx, func(r CustomListRepository) error {
				right, err := ast_eval.EvaluateNode(ctx, node, ast_eval.Runtime{TenantID: tenant.String(), CustomListRepo: r})
				if err == nil && !reflect.DeepEqual(left, right) {
					t.Fatal("list evaluation mismatch", left, right)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	missing := uuid.NewString()
	if _, err := source.GetListByID(ctx, tenant.String(), missing); !errors.Is(err, ports.ErrCustomListNotFound) || !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("source absence contract", err)
	}
	if err := target.ReadSnapshot(ctx, func(r CustomListRepository) error {
		if _, err := r.GetListByID(ctx, tenant.String(), missing); !errors.Is(err, ports.ErrCustomListNotFound) || !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("target absence contract", err)
		}
		if _, err := r.GetListByID(ctx, uuid.NewString(), lists[0].ID); !errors.Is(err, ErrTenantScope) || errors.Is(err, ports.ErrCustomListNotFound) {
			t.Fatal("foreign tenant became absence", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := source.GetListByID(ctx, uuid.NewString(), lists[0].ID); !errors.Is(err, ports.ErrCustomListNotFound) {
		t.Fatal("source leaked foreign list", err)
	}
}
