package mysqlruntime

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestLocalParameterInterpolationMatchesPreparedTransport(t *testing.T) {
	root, database, _, _ := testsupport.DecisionMySQLRuntimeDatabase(t)
	schema, _ := mysqlstore.Identifier(database)
	if _, err := root.Exec("CREATE TABLE " + schema + ".parameter_transport(id BINARY(16) PRIMARY KEY,b LONGBLOB NOT NULL,t LONGTEXT CHARACTER SET utf8mb4 NOT NULL,n INT NOT NULL,yes BOOLEAN NOT NULL,at DATETIME(6) NOT NULL,j JSON NOT NULL,optional JSON NULL)"); err != nil {
		t.Fatal(err)
	}
	for _, interpolate := range []bool{false, true} {
		t.Run(strconv.FormatBool(interpolate), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: os.Getenv("DECISION_MYSQL_TEST_ADDRESS"), Database: database, User: "root", Password: "isolated-test-root-only", AllowInsecureTransport: true, InterpolateParameters: interpolate, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			prepares := func() int {
				t.Helper()
				var key string
				var count int
				if err := conn.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE 'Com_stmt_prepare'").Scan(&key, &count); err != nil {
					t.Fatal(err)
				}
				return count
			}
			before := prepares()
			id := mysqlstore.UUID{0, 39, 92, 255, 127, 128, 34, 26, 10, 13, 1, 2, 3, 4, 5, 6}
			if interpolate {
				id[15] = 7
			}
			blob := make([]byte, 256)
			for i := range blob {
				blob[i] = byte(i)
			}
			text := "' OR 1=1; -- \\\"\r\n\x1a café 😀 " + strings.Repeat("é", 32000)
			at := time.Date(2026, 11, 6, 12, 1, 2, 345678000, time.UTC)
			tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(ctx, "INSERT INTO "+schema+".parameter_transport VALUES(?,?,?,?,?,?,?,?)", id, blob, mysqlstore.Text(text), mysqlstore.Int32(-2147483648), mysqlstore.Bool(true), mysqlstore.DateTime{Time: at}, mysqlstore.JSON(`{"precision":9007199254740993,"unicode":"😀"}`), nil); err != nil {
				t.Fatal(err)
			}
			var gotID mysqlstore.UUID
			var gotBlob []byte
			var gotText mysqlstore.Text
			var n mysqlstore.Int32
			var yes mysqlstore.Bool
			var gotAt mysqlstore.DateTime
			var precision string
			var optional sql.NullString
			if err := tx.QueryRowContext(ctx, "SELECT id,b,t,n,yes,at,JSON_EXTRACT(j,'$.precision'),optional FROM "+schema+".parameter_transport WHERE id=?", id).Scan(&gotID, &gotBlob, &gotText, &n, &yes, &gotAt, &precision, &optional); err != nil {
				t.Fatal(err)
			}
			if gotID != id || !bytes.Equal(gotBlob, blob) || string(gotText) != text || n != -2147483648 || !bool(yes) || !gotAt.Time.Equal(at) || precision != "9007199254740993" || optional.Valid {
				t.Fatal("parameter transport changed binary/text/scalar/JSON/time/null semantics")
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			tx, err = conn.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+schema+".parameter_transport WHERE id=?", id); err != nil {
				t.Fatal(err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".parameter_transport WHERE id=?", id).Scan(&count); err != nil || count != 1 {
				t.Fatal("rollback lost", count, err)
			}
			delta := prepares() - before
			t.Logf("interpolation=%t one-shot server prepares=%d", interpolate, delta)
			if interpolate && delta != 0 || !interpolate && delta == 0 {
				t.Fatal("one-shot prepare round trips", interpolate, delta)
			}
			cancelled, stop := context.WithCancel(ctx)
			stop()
			if err := conn.QueryRowContext(cancelled, "SELECT ?", 1).Scan(&count); !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation", err)
			}
			stmt, err := conn.PrepareContext(ctx, "SELECT ?")
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			if err := stmt.QueryRowContext(ctx, 42).Scan(&count); err != nil || count != 42 {
				t.Fatal("explicit prepared statements unavailable", count, err)
			}
		})
	}
}
