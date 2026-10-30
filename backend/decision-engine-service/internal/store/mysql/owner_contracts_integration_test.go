package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLCaseInboxConsumerContractFailsClosed(t *testing.T) {
	f := platformTestDB(t)
	ctx := context.Background()
	tenant, inbox := uuid.New(), uuid.New()
	session := TrustedDecisionSession{ActorID: uuid.New(), TenantID: tenant, SessionID: uuid.New(), MembershipVersion: 5}
	c, err := NewInboxContract(f.database, session)
	if err != nil {
		t.Fatal(err)
	}
	call := func(inbox uuid.UUID, write, active bool) error {
		return mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error { return c.Authorize(ctx, tx, tenant, inbox, write, active) })
	}
	if err := call(inbox, true, true); !errors.Is(err, ErrOwnerContractUnavailable) {
		t.Fatal("missing provider permitted", err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	stmt := "CREATE FUNCTION " + schema + ".case_manager_authorize_decision_inbox_v1(actor BINARY(16),tenant BINARY(16),session BINARY(16),version BIGINT,inbox BINARY(16),active BOOLEAN,locks BOOLEAN) RETURNS TINYINT DETERMINISTIC RETURN CASE WHEN actor=X'" + stripUUID(session.ActorID) + "' AND tenant=X'" + stripUUID(tenant) + "' AND session=X'" + stripUUID(session.SessionID) + "' AND inbox=X'" + stripUUID(inbox) + "' AND version=5 AND active=1 AND locks=1 THEN 1 ELSE 0 END"
	if _, err := f.db.ExecContext(ctx, stmt); err != nil {
		t.Fatal(err)
	}
	if err := call(inbox, true, true); err != nil {
		t.Fatal(err)
	}
	if err := call(uuid.New(), true, true); !errors.Is(err, ErrDecisionAuthorizationDenied) {
		t.Fatal("wrong inbox", err)
	}
	if err := call(inbox, false, true); !errors.Is(err, ErrDecisionAuthorizationDenied) {
		t.Fatal("lock argument not forwarded", err)
	}
}
func TestMySQLScreeningSourceConsumerContractBindings(t *testing.T) {
	f := platformTestDB(t)
	ctx := context.Background()
	tenant, source, decision, parent, config, execution := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	c, err := NewScreeningSourceContract(f.database, 1024)
	if err != nil {
		t.Fatal(err)
	}
	read := func(tenant, decision uuid.UUID) (ScreeningSource, error) {
		var out ScreeningSource
		err := mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error {
			var err error
			out, err = c.Lock(ctx, tx, tenant, source, decision, config)
			return err
		})
		return out, err
	}
	if _, err := read(tenant, decision); !errors.Is(err, ErrOwnerContractUnavailable) {
		t.Fatal("missing source provider", err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err := f.db.ExecContext(ctx, "CREATE TABLE "+schema+".fixture_screening_source(id BINARY(16),tenant_id BINARY(16),decision_id BINARY(16),scenario_id BINARY(16),config_id BINARY(16),execution_id BINARY(16),projection_version BIGINT,status VARCHAR(32),updated_at DATETIME(6),sent_at DATETIME(6) NULL,completed_at DATETIME(6) NULL,failed_at DATETIME(6) NULL,response_json JSON,provider_reference LONGTEXT,last_error LONGTEXT,PRIMARY KEY(tenant_id,id)) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "CREATE ALGORITHM=MERGE VIEW "+schema+".screening_decision_source_v1 AS SELECT * FROM "+schema+".fixture_screening_source"); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".fixture_screening_source VALUES(?,?,?,?,?,?,3,'no_hit',?,NULL,?,NULL,JSON_OBJECT('match_count',0),'ref','')", mysqlstore.UUID(source), mysqlstore.UUID(tenant), mysqlstore.UUID(decision), mysqlstore.UUID(parent), mysqlstore.UUID(config), mysqlstore.UUID(execution), at, at); err != nil {
		t.Fatal(err)
	}
	out, err := read(tenant, decision)
	if err != nil || out.Version != 3 || out.Status != "no_hit" || !out.CompletedAt.Valid || uuid.UUID(out.ExecutionID.UUID) != execution {
		t.Fatal(out, err)
	}
	if _, err := read(tenant, uuid.New()); !errors.Is(err, ErrOwnerRelationshipConflict) {
		t.Fatal("relationship mismatch", err)
	}
	if _, err := read(uuid.New(), decision); !errors.Is(err, ErrOwnerContractUnavailable) {
		t.Fatal("tenant source leaked", err)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_screening_source SET projection_version=0"); err != nil {
		t.Fatal(err)
	}
	if _, err := read(tenant, decision); !errors.Is(err, ErrOwnerRelationshipConflict) {
		t.Fatal("zero source version", err)
	}
}
