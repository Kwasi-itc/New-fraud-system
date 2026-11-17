package privatelists

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CheckDatabaseBinding rejects a restored clone even though a logical backup
// deliberately preserves its database_identity UUID. Both pools must resolve to
// the same primary server/database, not merely share copied application rows.
func CheckDatabaseBinding(ctx context.Context, reader, writer *pgxpool.Pool) error {
	denied := errors.New("private-list writer must use the same migrated primary application database")
	if reader == nil || writer == nil {
		return denied
	}
	type binding struct {
		ID, Database, OID, Address, Port, Started string
		Replica                                   bool
	}
	read := func(db *pgxpool.Pool) (binding, error) {
		var b binding
		e := db.QueryRow(ctx, `SELECT id::text,current_database(),(SELECT oid::text FROM pg_catalog.pg_database WHERE datname=current_database()),coalesce(inet_server_addr()::text,''),coalesce(inet_server_port()::text,''),extract(epoch FROM pg_postmaster_start_time())::text,pg_is_in_recovery() FROM screening.database_identity WHERE singleton`).Scan(&b.ID, &b.Database, &b.OID, &b.Address, &b.Port, &b.Started, &b.Replica)
		return b, e
	}
	main, e := read(reader)
	if e != nil {
		return denied
	}
	private, e := read(writer)
	if e != nil || main != private || main.Replica {
		return denied
	}
	return nil
}
