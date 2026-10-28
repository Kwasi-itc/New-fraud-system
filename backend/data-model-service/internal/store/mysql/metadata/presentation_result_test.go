package metadata_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func restrictedPresentationPool(t *testing.T, ctx context.Context, db *sql.DB, database string) (*sql.DB, string) {
	t.Helper()
	principal := installation.Definer{User: "pr_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24], Host: "%"}
	account, err := principal.SQLAccount()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "CREATE USER "+account+" IDENTIFIED BY 'isolated-presentation-only'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, e := db.Exec("DROP USER " + account); e != nil {
			t.Error(e)
		}
	})
	policy, err := installation.PresentationOwnerPolicy(principal, false)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := installation.GrantStatements(database, policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range grants {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = installation.VerifyAccountPolicy(ctx, conn, database, policy); err != nil {
		t.Fatal(err)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	pool, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS"), Database: database, User: principal.User, Password: "isolated-presentation-only", AllowInsecureTransport: true, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, q := range []string{"UPDATE data_model_fields SET nullable=TRUE", "UPDATE data_model_fields SET is_unique=FALSE", "UPDATE data_model_tables SET archived=TRUE", "UPDATE data_model_tables SET caption_field='forged'", "UPDATE data_model_physical_tables SET write_state='open'", "UPDATE data_model_owner_results SET result_kind='forged'", "DELETE FROM data_model_publication_operations", "SELECT * FROM data_model_exact_keys", "CREATE TABLE forbidden_presentation (id INT)"} {
		if _, err = pool.ExecContext(ctx, q); err == nil {
			t.Fatal("presentation privilege bypass", q)
		}
	}
	return pool, account
}
