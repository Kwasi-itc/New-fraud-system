package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLCaseInboxBatchConsumerContractFailsClosed(t *testing.T) {
	f := platformTestDB(t)
	ctx := context.Background()
	tenant := uuid.New()
	session := TrustedDecisionSession{ActorID: uuid.New(), TenantID: tenant, SessionID: uuid.New(), MembershipVersion: 7}
	c, err := NewInboxContract(f.database, session)
	if err != nil {
		t.Fatal(err)
	}
	first, second := uuid.MustParse("00000000-0000-0000-0000-000000000001"), uuid.MustParse("00000000-0000-0000-0000-000000000002")
	call := func(c InboxContract, tenant uuid.UUID, ids []uuid.UUID, write, active bool) error {
		return mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error { return c.AuthorizeBatch(ctx, tx, tenant, ids, write, active) })
	}
	if err := call(c, tenant, []uuid.UUID{first}, true, true); !errors.Is(err, ErrOwnerContractUnavailable) {
		t.Fatal("missing provider allowed", err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	// Contract fixture ONLY: confirms exact bindings and sorted bounded input.
	// The real case owner must provide permissions/revocation and row locking.
	stmt := "CREATE FUNCTION " + schema + ".case_manager_authorize_decision_inboxes_v1(actor BINARY(16),tenant BINARY(16),session BINARY(16),version BIGINT,ids JSON,active BOOLEAN,locks BOOLEAN) RETURNS TINYINT DETERMINISTIC NO SQL RETURN actor=X'" + stripUUID(session.ActorID) + "' AND tenant=X'" + stripUUID(tenant) + "' AND session=X'" + stripUUID(session.SessionID) + "' AND version=7 AND active=1 AND locks=1 AND JSON_LENGTH(ids)<=500 AND (JSON_LENGTH(ids)=0 OR JSON_UNQUOTE(JSON_EXTRACT(ids,'$[0]'))='" + first.String() + "')"
	if _, err := f.db.ExecContext(ctx, stmt); err != nil {
		t.Fatal(err)
	}
	if err := call(c, tenant, []uuid.UUID{second, first}, true, true); err != nil {
		t.Fatal("ordered batch", err)
	}
	if err := call(c, tenant, nil, true, true); err != nil {
		t.Fatal("empty batch must be []", err)
	}
	for _, test := range []struct {
		tenant        uuid.UUID
		ids           []uuid.UUID
		write, active bool
		want          error
	}{
		{uuid.New(), []uuid.UUID{first}, true, true, ErrDecisionAuthorizationDenied},
		{tenant, []uuid.UUID{first}, false, true, ErrDecisionAuthorizationDenied},
		{tenant, []uuid.UUID{first}, true, false, ErrDecisionAuthorizationDenied},
		{tenant, []uuid.UUID{first, first}, true, true, ErrTenantScope},
		{tenant, []uuid.UUID{uuid.Nil}, true, true, ErrTenantScope},
		{tenant, make([]uuid.UUID, 501), true, true, ErrProjectionLimit},
	} {
		if err := call(c, test.tenant, test.ids, test.write, test.active); !errors.Is(err, test.want) {
			t.Fatal("authorization", err, test.want)
		}
	}
	for _, readOnly := range []bool{false, true} {
		changed := session
		changed.ReadOnly = readOnly
		if !readOnly {
			changed.MembershipVersion++
		}
		contract, err := NewInboxContract(f.database, changed)
		if err != nil {
			t.Fatal(err)
		}
		if err := call(contract, tenant, []uuid.UUID{first}, true, true); !errors.Is(err, ErrDecisionAuthorizationDenied) {
			t.Fatal("stale/read-only session allowed", err)
		}
	}
	if _, err := f.db.ExecContext(ctx, "DROP FUNCTION "+schema+".case_manager_authorize_decision_inboxes_v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "CREATE FUNCTION "+schema+".case_manager_authorize_decision_inboxes_v1(actor BINARY(16),tenant BINARY(16),session BINARY(16),version BIGINT,ids JSON,active BOOLEAN,locks BOOLEAN) RETURNS TINYINT DETERMINISTIC RETURN NULL"); err != nil {
		t.Fatal(err)
	}
	if err := call(c, tenant, []uuid.UUID{first}, true, true); !errors.Is(err, ErrOwnerContractUnavailable) {
		t.Fatal("NULL provider allowed", err)
	}
}
