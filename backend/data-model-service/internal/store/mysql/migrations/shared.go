// Package migrations binds the shared migration executor to the data-model
// owner's existing ledger. Frozen installation statements and checksums are
// unchanged; other owners must select their own namespace through mysqlstore.
package migrations

import (
	"context"
	"database/sql"

	shared "github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

type Step = shared.Step
type Plan = shared.Plan
type History = shared.History
type Runner = shared.Runner

var ErrDrift = shared.ErrDrift
var ErrLineage = shared.ErrLineage

func New(db *sql.DB, plan Plan) (*Runner, error) { return shared.New(db, "data_model", plan) }

func Definition(ctx context.Context, conn *sql.Conn, database, kind, name string) (string, error) {
	return shared.Definition(ctx, conn, database, kind, name)
}
