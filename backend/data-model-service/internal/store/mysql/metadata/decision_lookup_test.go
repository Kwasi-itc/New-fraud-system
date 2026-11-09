package metadata_test

import (
	"context"
	"database/sql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func installDecisionRecordLookup(t *testing.T, ctx context.Context, db *sql.DB, database string, installed migrations.Plan) {
	t.Helper()
	check := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	definer := installation.Definer{User: "dl_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20], Host: "%"}
	create, e := installation.CreateDefiner(definer)
	check(e)
	_, e = db.ExecContext(ctx, create)
	check(e)
	account, e := definer.SQLAccount()
	check(e)
	t.Cleanup(func() {
		if _, e := db.ExecContext(context.Background(), "DROP USER "+account); e != nil {
			t.Error(e)
		}
	})
	reference, refDatabase := metadataDB(t)
	refSteps, e := installation.RecordLookupUpgradeStatements(refDatabase, definer)
	check(e)
	steps, e := installation.RecordLookupUpgradeStatements(database, definer)
	check(e)
	conn, e := reference.Conn(ctx)
	check(e)
	for i, step := range refSteps {
		_, e = conn.ExecContext(ctx, step.SQL)
		check(e)
		definition, e := migrations.Definition(ctx, conn, refDatabase, step.Kind, step.Name)
		check(e)
		steps[i].After = strings.ReplaceAll(definition, refDatabase, database)
	}
	check(conn.Close())
	plan, e := installation.ExtendRecordLookupPlan(installed, definer, steps)
	check(e)
	runner, e := migrations.New(db, plan)
	check(e)
	check(runner.Apply(ctx))
	grants, e := installation.GrantStatements(database, installation.RecordLookupDefinerPolicy(definer))
	check(e)
	for _, grant := range grants {
		_, e = db.ExecContext(ctx, grant)
		check(e)
	}
	check(runner.Verify(ctx,func(conn *sql.Conn)error{return installation.VerifyAccountPolicy(ctx,conn,database,installation.RecordLookupDefinerPolicy(definer))}))
	check(runner.Apply(ctx))
}
