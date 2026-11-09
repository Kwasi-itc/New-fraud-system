package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

var ErrPhysicalTextDuplicate = errors.New("existing duplicate text values prevent uniqueness")

type physicalTextSession interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// keyBatch uses the primary key, never OFFSET, and returns IDs only. All text
// remains inside MySQL. The caller closes rows before issuing another query.
func textKeyBatch(ctx context.Context, q physicalTextSession, qualified string, after []byte) ([][]byte, error) {
	query := "SELECT id FROM " + qualified
	var args []any
	if after != nil {
		query += " WHERE id>?"
		args = append(args, after)
	}
	rows, err := q.QueryContext(ctx, query+" ORDER BY id LIMIT 256", args...)
	if err != nil {
		return nil, err
	}
	var ids [][]byte
	for rows.Next() {
		var id []byte
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	return ids, errors.Join(err, rows.Err(), rows.Close())
}

// CheckTextDuplicates runs while preparation owns the target writer barrier.
// Two session-local InnoDB indexes hold only IDs and digest candidates, never
// text payloads. Candidate matches are confirmed against complete binary values
// through primary-key lookups. This avoids LONGTEXT GROUP BY truncation and
// hash-only equality. Use PreparePhysicalFieldChange so cancellation always
// discards the session, including temporary tables that cannot be dropped by a canceled Tx.
func CheckTextDuplicates(ctx context.Context, tx *sql.Tx, database, table, column string) (result error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return err
	}
	name, err := mysqlstore.Identifier(table)
	if err != nil {
		return err
	}
	value, err := mysqlstore.Identifier(column)
	if err != nil {
		return err
	}
	qualified := schema + "." + name
	var created []string
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		for _, name := range created {
			_, err := tx.ExecContext(cleanup, "DROP TEMPORARY TABLE "+name)
			result = errors.Join(result, err)
		}
	}()
	for _, name := range []string{"dm_text_seen", "dm_text_batch"} {
		_, err := tx.ExecContext(ctx, "CREATE TEMPORARY TABLE "+name+" (id BINARY(16) NOT NULL PRIMARY KEY,digest BINARY(32) NOT NULL,INDEX digest_id(digest,id)) ENGINE=InnoDB")
		if err != nil {
			return err
		}
		created = append(created, name)
	}
	var after []byte
	for {
		ids, err := textKeyBatch(ctx, tx, qualified, after)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM dm_text_batch"); err != nil {
			return err
		}
		upper := ids[len(ids)-1]
		where := "id<=? AND " + value + " IS NOT NULL"
		args := []any{upper}
		if after != nil {
			where += " AND id>?"
			args = append(args, after)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO dm_text_batch(id,digest) SELECT id,UNHEX(SHA2(CAST("+value+" AS BINARY),256)) FROM "+qualified+" WHERE "+where, args...); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO dm_text_seen(id,digest) SELECT id,digest FROM dm_text_batch"); err != nil {
			return err
		}
		var duplicate int
		err = tx.QueryRowContext(ctx, textDuplicateProbe(qualified, value)).Scan(&duplicate)
		if err == nil {
			return ErrPhysicalTextDuplicate
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		after = upper
	}
}

func textDuplicateProbe(qualified, value string) string {
	return "SELECT 1 FROM dm_text_batch c STRAIGHT_JOIN dm_text_seen s FORCE INDEX(digest_id) ON s.digest=c.digest AND s.id<c.id STRAIGHT_JOIN " + qualified + " a ON a.id=c.id STRAIGHT_JOIN " + qualified + " b ON b.id=s.id WHERE CAST(a." + value + " AS BINARY)=CAST(b." + value + " AS BINARY) LIMIT 1"
}

// The writer barrier is durably closed. Each UPDATE is one short atomic batch:
// triggers derive exact keys and the unique index rejects duplicates. Existing
// non-NULL keys are skipped, so restart after a lost batch response is safe.
func backfillTextKeys(ctx context.Context, conn *sql.Conn, database, name string, plan PhysicalDDLPlan, step int) error {
	if plan.Native == nil || plan.Native.Changes[step].Kind != "text_unique_check" {
		return nil
	}
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return err
	}
	table, err := mysqlstore.Identifier(name)
	if err != nil {
		return err
	}
	column, err := mysqlstore.Identifier(plan.TextBackfillColumn())
	if err != nil {
		return err
	}
	key := "`record_text_key_" + physicalUUID(plan.Native.Changes[step].FieldID) + "`"
	qualified := schema + "." + table
	var after []byte
	for {
		ids, err := textKeyBatch(ctx, conn, qualified, after)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		if _, err = conn.ExecContext(ctx, "UPDATE "+qualified+" SET "+column+"="+column+" WHERE id IN ("+marks+") AND "+key+" IS NULL AND "+column+" IS NOT NULL", args...); err != nil {
			return err
		}
		after = ids[len(ids)-1]
	}
}
