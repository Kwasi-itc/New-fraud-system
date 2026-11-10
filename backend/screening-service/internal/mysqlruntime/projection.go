package mysqlruntime

import (
	"context"
	"database/sql"
	"errors"
	decision "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/mysqlprojection"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlpublication"
	"github.com/google/uuid"
)

func ProjectBatch(ctx context.Context, db *sql.DB, database string) error {
	schema, e := mysqlstore.Identifier(database)
	if e != nil {
		return e
	}
	rows, e := db.QueryContext(ctx, "SELECT DISTINCT q.tenant_id FROM "+schema+".screening_execution_projection_queue q JOIN "+schema+".identity_tenants t ON t.id=q.tenant_id AND t.active WHERE q.available_at<=UTC_TIMESTAMP(6) ORDER BY q.tenant_id LIMIT 25")
	if e != nil {
		return e
	}
	tenants := []uuid.UUID{}
	for rows.Next() {
		var v mysqlstore.UUID
		if e = rows.Scan(&v); e != nil {
			rows.Close()
			return e
		}
		tenants = append(tenants, uuid.UUID(v))
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, tenant := range tenants {
		authorize := func(ctx context.Context, tx *sql.Tx, t uuid.UUID, write bool) error {
			if t != tenant {
				return errors.New("projection tenant mismatch")
			}
			var active bool
			q := "SELECT active FROM " + schema + ".identity_tenants WHERE id=?"
			if write {
				q += " FOR SHARE"
			}
			if e := tx.QueryRowContext(ctx, q, mysqlstore.UUID(t)).Scan(&active); e != nil {
				return e
			}
			if !active {
				return errors.New("projection tenant is inactive")
			}
			return nil
		}
		source, e := mysqlpublication.New(db, database, tenant, authorize)
		if e != nil {
			return e
		}
		sink, e := decision.NewScreeningSink(db, database, tenant, authorize)
		if e != nil {
			return e
		}
		projector, e := mysqlpublication.NewProjector(source, database, sink)
		if e != nil {
			return e
		}
		if _, e = projector.RunBatch(ctx, 25); e != nil {
			return e
		}
	}
	return nil
}
