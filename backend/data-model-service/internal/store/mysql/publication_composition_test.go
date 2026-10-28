package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"testing"

	"github.com/google/uuid"
)

func TestPublicationOwnerTransactionValidation(t *testing.T) {
	s := &PublicationStore{}
	tenant, table := uuid.New(), uuid.New()
	op := PublicationOperation{ID: uuid.New(), RequestHash: sha256.Sum256([]byte("request")), Condition: PublicationCondition{Mode: PublicationAnyVersion}}
	callback := func(*sql.Tx) error { t.Fatal("invalid request reached mutation"); return nil }
	for _, tc := range []struct {
		tx            *sql.Tx
		tenant, table uuid.UUID
		op            PublicationOperation
		mutate        func(*sql.Tx) error
	}{
		{nil, tenant, table, op, callback},
		{&sql.Tx{}, uuid.Nil, table, op, callback},
		{&sql.Tx{}, tenant, uuid.Nil, op, callback},
		{&sql.Tx{}, tenant, table, op, nil},
		{&sql.Tx{}, tenant, table, PublicationOperation{}, callback},
	} {
		v, err := s.MutateAndPublishOperationTx(context.Background(), tc.tx, tc.tenant, tc.table, tc.op, tc.mutate)
		if err == nil || v.Version != uuid.Nil {
			t.Fatalf("invalid input accepted: %+v %v", v, err)
		}
	}
}
