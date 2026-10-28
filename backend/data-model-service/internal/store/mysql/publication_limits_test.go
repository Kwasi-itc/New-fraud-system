package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestPublicationByteLimitConfiguration(t *testing.T) {
	for _, limit := range []int{-1, 0, (64 << 20) + 1} {
		if _, err := NewPublicationStore(&sql.DB{}, "fixture", 10, 10, limit); err == nil {
			t.Fatalf("accepted invalid byte limit %d", limit)
		}
	}
	for _, limit := range []int{1, 64 << 20} {
		if _, err := NewPublicationStore(&sql.DB{}, "fixture", 10, 10, limit); err != nil {
			t.Fatal(err)
		}
	}
}

func testPublicationByteLimits(t *testing.T, db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store := func(limit int) *PublicationStore {
		t.Helper()
		s, err := NewPublicationStore(db, "data_model_publication_test", 100, 100, limit)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	seed := func(name string) (uuid.UUID, uuid.UUID) {
		t.Helper()
		tenant, table := uuid.New(), uuid.New()
		if _, err := db.ExecContext(ctx, "INSERT INTO data_model_tables(id,tenant_id,name) VALUES (?,?,?)", mysqlstore.UUID(table), mysqlstore.UUID(tenant), name); err != nil {
			t.Fatal(err)
		}
		return tenant, table
	}
	op := func() PublicationOperation {
		return PublicationOperation{ID: uuid.New(), RequestHash: sha256.Sum256([]byte("limit-test")), Condition: PublicationCondition{Mode: PublicationAnyVersion}}
	}
	noChange := func(*sql.Tx) error { return nil }
	for _, tc := range []struct {
		name, table string
		fields      []string
		labels      []string
		limit       int
		fail        bool
	}{
		{"table_bytes_not_characters", "界界", nil, nil, 5, true},
		{"table_exact_boundary", "界界", nil, nil, 6, false},
		{"field_row", "t", []string{strings.Repeat("f", 30)}, nil, 20, true},
		{"fields_cumulative", "t", []string{"aaaa", "bbbb"}, nil, 20, true},
		{"fields_exact_boundary", "t", []string{"aaaa", "bbbb"}, nil, 21, false},
		{"enum_row", "t", []string{"f"}, []string{strings.Repeat("e", 30)}, 20, true},
		{"enums_cumulative", "t", []string{"f"}, []string{"aaaaaa", "bbbbbb"}, 20, true},
		{"enums_exact_boundary", "t", []string{"f"}, []string{"aaaaaa", "bbbbbb"}, 22, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenant, table := seed(tc.table)
			err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				for _, name := range tc.fields {
					field := uuid.New()
					if _, err := tx.ExecContext(ctx, "INSERT INTO data_model_fields(id,tenant_id,table_id,name,data_type) VALUES (?,?,?,?,?)", mysqlstore.UUID(field), mysqlstore.UUID(tenant), mysqlstore.UUID(table), name, "string"); err != nil {
						return err
					}
					for _, label := range tc.labels {
						if _, err := tx.ExecContext(ctx, "INSERT INTO data_model_field_enum_values(id,tenant_id,field_id,value,label,sort_order) VALUES (?,?,?,?,?,0)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), mysqlstore.UUID(field), "v", label); err != nil {
							return err
						}
					}
				}
				_, _, _, err := store(tc.limit).snapshot(ctx, tx, tenant, table)
				return err
			})
			if tc.fail && !errors.Is(err, ErrSnapshotLimit) || !tc.fail && err != nil {
				t.Fatal("unexpected snapshot result", err)
			}
			if tc.fail {
				var fields, enums int
				if err := db.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM data_model_fields WHERE table_id=?),(SELECT count(*) FROM data_model_field_enum_values WHERE tenant_id=?)", mysqlstore.UUID(table), mysqlstore.UUID(tenant)).Scan(&fields, &enums); err != nil || fields != 0 || enums != 0 {
					t.Fatal("snapshot failure leaked metadata", err)
				}
			}
		})
	}

	t.Run("encoded_expansion_rolls_back_every_write", func(t *testing.T) {
		tenant, table := seed("records")
		operation := op()
		p, err := store(256).MutateAndPublishOperation(ctx, tenant, table, operation, func(tx *sql.Tx) error {
			// Raw text fits, but JSON escaping makes the publication too large.
			_, err := tx.ExecContext(ctx, "UPDATE data_model_tables SET name=? WHERE id=?", strings.Repeat("<", 100), mysqlstore.UUID(table))
			return err
		})
		if !errors.Is(err, ErrSnapshotLimit) || p.Payload != nil || p.Version != uuid.Nil {
			t.Fatal("oversized publication accepted", err)
		}
		var name string
		var contracts, receipts int
		if err := db.QueryRowContext(ctx, "SELECT name,(SELECT count(*) FROM data_model_ingestion_contracts WHERE table_id=?),(SELECT count(*) FROM data_model_publication_operations WHERE table_id=?) FROM data_model_tables WHERE id=?", mysqlstore.UUID(table), mysqlstore.UUID(table), mysqlstore.UUID(table)).Scan(&name, &contracts, &receipts); err != nil {
			t.Fatal(err)
		}
		if name != "records" || contracts != 0 || receipts != 0 {
			t.Fatal("byte limit failure leaked mutation, publication or receipt")
		}
	})

	t.Run("stored_json_boundary_and_recovery", func(t *testing.T) {
		tenant, table := seed("records")
		operation := op()
		first, err := store(4096).MutateAndPublishOperation(ctx, tenant, table, operation, noChange)
		if err != nil {
			t.Fatal(err)
		}
		var stored int
		if err := db.QueryRowContext(ctx, "SELECT OCTET_LENGTH(payload) FROM data_model_ingestion_contracts WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table)).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored <= len(first.Payload) {
			t.Fatal("fixture must exercise MySQL JSON text expansion")
		}
		neverMutate := func(*sql.Tx) error {
			t.Error("oversized prior/receipt invoked mutation")
			return errors.New("unexpected mutation")
		}
		bounded := store(stored - 1)
		if p, err := bounded.MutateAndPublish(ctx, tenant, table, neverMutate); !errors.Is(err, ErrSnapshotLimit) || p.Payload != nil {
			t.Fatal("oversized prior accepted", err)
		}
		if p, err := bounded.MutateAndPublishOperation(ctx, tenant, table, operation, neverMutate); !errors.Is(err, ErrSnapshotLimit) || p.Payload != nil {
			t.Fatal("oversized replay accepted", err)
		}
		if p, err := bounded.RecoverPublication(ctx, tenant, table, operation); !errors.Is(err, ErrSnapshotLimit) || p.Payload != nil {
			t.Fatal("oversized recovery accepted", err)
		}
		// Restoring the supported limit recovers the original receipt unchanged.
		for _, recover := range []bool{false, true} {
			p, err := store(stored).RecoverPublication(ctx, tenant, table, operation)
			if !recover {
				p, err = store(stored).MutateAndPublishOperation(ctx, tenant, table, operation, neverMutate)
			}
			if err != nil || p.Version != first.Version {
				t.Fatal("exact boundary replay/recovery failed", err)
			}
		}
		if p, err := store(stored).MutateAndPublish(ctx, tenant, table, noChange); err != nil || p.Version != first.Version {
			t.Fatal("exact boundary prior failed", err)
		}
		// A compact Go payload can fit while MySQL's stored representation does not.
		otherTenant, otherTable := seed("records")
		if p, err := bounded.MutateAndPublish(ctx, otherTenant, otherTable, noChange); !errors.Is(err, ErrSnapshotLimit) || p.Payload != nil {
			t.Fatal("unrecoverable stored JSON accepted", err)
		}
		if _, err := store(stored).MutateAndPublish(ctx, otherTenant, otherTable, noChange); err != nil {
			t.Fatal("exact stored boundary failed", err)
		}
	})
}
