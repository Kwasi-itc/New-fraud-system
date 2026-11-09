package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

// ModelReadAuthorizer checks trusted identity and tenant read access using the
// supplied read-only snapshot. It must not lock or mutate identity rows.
type ModelReadAuthorizer func(context.Context, *sql.Tx, uuid.UUID) error

// NewAuthorizedPublishedModelReader requires trusted local SQL read authority.
// The callback runs before metadata reads in their read-only transaction and
// must validate the caller and tenant from trusted request context. It must not
// perform writes, acquire mutation locks, or make network calls. Authorization
// has snapshot semantics; physical publication is rechecked by the consumer in
// its own data transaction.
func NewAuthorizedPublishedModelReader(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, authorize ModelReadAuthorizer) (*DataModelReadRepository, error) {
	if authorize == nil {
		return nil, errors.New("trusted model read authority required")
	}
	r, err := NewPublishedDataModelReadRepository(db, database, tenant, maxRows, maxBytes)
	if err != nil {
		return nil, err
	}
	r.authorize = authorize
	return r, nil
}
