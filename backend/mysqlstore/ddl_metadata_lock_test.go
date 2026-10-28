package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestMySQLDDLMetadataLockRecovery(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		label := "server_timeout"
		if cancelRequest {
			label = "request_cancel"
		}
		t.Run(label, func(t *testing.T) {
			db := integrationDB(t)
			ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			name := fmt.Sprintf("ddl_metadata_%d", time.Now().UnixNano())
			table, err := Identifier(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, "CREATE TABLE "+table+" (id INT PRIMARY KEY) ENGINE=InnoDB"); err != nil {
				t.Fatal(err)
			}
			reader, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Rollback()
			var count int
			if err := reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
				t.Fatal(err)
			}
			request, cancel := context.WithCancel(ctx)
			defer cancel()
			connection := make(chan int64, 1)
			result := make(chan error, 1)
			go func() {
				result <- WithDDLLock(request, db, name, func(conn *sql.Conn) error {
					var id int64
					if err := conn.QueryRowContext(request, "SELECT CONNECTION_ID()").Scan(&id); err != nil {
						return err
					}
					// Bound server work independently of client socket cancellation.
					if _, err := conn.ExecContext(request, "SET SESSION lock_wait_timeout=2"); err != nil {
						return err
					}
					connection <- id
					_, err := conn.ExecContext(request, "ALTER TABLE "+table+" ADD COLUMN revision INT NULL")
					return err
				})
			}()
			var id int64
			select {
			case id = <-connection:
			case err := <-result:
				t.Fatalf("DDL setup failed: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				var waiting int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID=? AND STATE='Waiting for table metadata lock'", id).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting == 1 {
					break
				}
				select {
				case err := <-result:
					t.Fatalf("DDL ended before observed metadata wait: %v", err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-ticker.C:
				}
			}
			if err := WithDDLLock(ctx, db, name, func(*sql.Conn) error {
				t.Error("second executor entered during blocked ALTER")
				return nil
			}); !errors.Is(err, ErrDDLLockBusy) {
				t.Fatalf("blocked DDL lost its named lock: %v", err)
			}
			if cancelRequest {
				cancel()
			}
			select {
			case err := <-result:
				if cancelRequest {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancellation cause lost: %v", err)
					}
				} else if Classify(err) != LockTimeout {
					t.Fatalf("metadata timeout not surfaced: %v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// Keep the reader open until the server has ended the DDL session.
			// A client cancellation alone is not evidence that server work ended.
			for {
				var free int
				if err := db.QueryRowContext(ctx, "SELECT IS_FREE_LOCK(?)", name).Scan(&free); err != nil {
					t.Fatal(err)
				}
				if free == 1 {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-ticker.C:
				}
			}
			if err := reader.Rollback(); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME='revision'", name).Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed ALTER changed shape: %d %v", count, err)
			}
			if err := WithDDLLock(ctx, db, name, func(conn *sql.Conn) error {
				_, err := conn.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN revision INT NULL")
				return err
			}); err != nil {
				t.Fatalf("explicit recovery failed: %v", err)
			}
			if db.Stats().InUse != 0 {
				t.Fatal("DDL recovery leaked a lease")
			}
		})
	}
}
