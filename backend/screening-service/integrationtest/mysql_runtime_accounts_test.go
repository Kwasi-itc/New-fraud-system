package integrationtest

import (
	"database/sql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	runtime "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/mysqlruntime"
	"github.com/google/uuid"
	"os"
	"strings"
	"testing"
	"time"
)

func (f *fixture) runtimeDB(t *testing.T, role string) *sql.DB {
	t.Helper()
	user := "screen_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	const password = "isolated-runtime-fixture-password"
	account := "`" + user + "`@'%'"
	_, e := f.db.ExecContext(f.ctx, "CREATE USER "+account+" IDENTIFIED BY '"+password+"'")
	must(t, e)
	t.Cleanup(func() {
		_, e := f.db.ExecContext(f.ctx, "DROP USER "+account)
		if e != nil {
			t.Error(e)
		}
	})
	must(t, runtime.ConfigureRuntimeGrants(f.ctx, f.db, f.database, role, user, "%"))
	db, e := mysqlstore.Open(f.ctx, mysqlstore.Config{Address: os.Getenv("SCREENING_MYSQL_TEST_ADDRESS"), Database: f.database, User: user, Password: password, AllowInsecureTransport: true, MaxOpen: 4, MaxIdle: 2, ConnectTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, LockWaitTimeout: 5 * time.Second})
	must(t, e)
	t.Cleanup(func() { db.Close() })
	if _, e = db.ExecContext(f.ctx, "CREATE TABLE `"+f.database+"`.runtime_must_not_create(id INT)"); e == nil {
		t.Fatal("runtime account unexpectedly has DDL privileges")
	}
	return db
}
