// Package mysqlprojection exposes the decision owner's bounded screening
// reconciliation contract to workers sharing the same logical database.
package mysqlprojection

import (
	"context"
	"database/sql"
	store "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

type Sink interface {
	Matches(*sql.DB, string, uuid.UUID) bool
	Prepare(context.Context, *sql.Tx, uuid.UUID) (func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error, error)
}

func NewScreeningSink(db *sql.DB, database string, tenant uuid.UUID, authorize func(context.Context, *sql.Tx, uuid.UUID, bool) error) (Sink, error) {
	source, e := store.NewScreeningSourceContract(database, 1<<20)
	if e != nil {
		return nil, e
	}
	owner, e := store.NewEffectStore(db, database, tenant, 500, 4<<20, store.PlatformAuthorizer(authorize), &source)
	if e != nil {
		return nil, e
	}
	return store.NewScreeningProjectionSink(owner)
}
