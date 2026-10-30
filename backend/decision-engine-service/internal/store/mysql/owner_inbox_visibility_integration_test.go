package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLCaseInboxBatchVisibilityContractFailsClosed(t *testing.T) {
	f := platformTestDB(t)
	ctx := context.Background()
	tenant := uuid.New()
	session := TrustedDecisionSession{ActorID: uuid.New(), TenantID: tenant, SessionID: uuid.New(), MembershipVersion: 4}
	c, err := NewInboxContract(f.database, session)
	if err != nil {
		t.Fatal(err)
	}
	first, second := uuid.New(), uuid.New()
	call := func(ids []uuid.UUID) (map[uuid.UUID]bool, error) {
		var result map[uuid.UUID]bool
		err := mysqlstore.Snapshot(ctx, f.db, func(tx *sql.Tx) error { var err error; result, err = c.Visible(ctx, tx, tenant, ids); return err })
		return result, err
	}
	if _, err := call([]uuid.UUID{first}); !errors.Is(err, ErrOwnerContractUnavailable) {
		t.Fatal("missing provider", err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err := f.db.ExecContext(ctx, "CREATE TABLE "+schema+".fixture_visibility(value JSON NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".fixture_visibility VALUES(JSON_ARRAY())"); err != nil {
		t.Fatal(err)
	}
	// Real providers must additionally enforce current auth/tenant permissions.
	stmt := "CREATE FUNCTION " + schema + ".case_manager_visible_decision_inboxes_v1(a BINARY(16),t BINARY(16),s BINARY(16),v BIGINT,ids JSON) RETURNS JSON READS SQL DATA RETURN IF(a=X'" + stripUUID(session.ActorID) + "' AND t=X'" + stripUUID(tenant) + "' AND s=X'" + stripUUID(session.SessionID) + "' AND v=4 AND JSON_LENGTH(ids)<=500,(SELECT value FROM " + schema + ".fixture_visibility),NULL)"
	if _, err := f.db.ExecContext(ctx, stmt); err != nil {
		t.Fatal(err)
	}
	set := func(value any) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_visibility SET value=?", mysqlstore.JSON(raw)); err != nil {
			t.Fatal(err)
		}
	}
	set([]string{first.String()})
	result, err := call([]uuid.UUID{second, first})
	if err != nil || len(result) != 1 || !result[first] {
		t.Fatal("bounded subset", result, err)
	}
	for _, test := range []struct {
		value any
		want  error
	}{
		{nil, ErrOwnerContractUnavailable}, {map[string]any{}, ErrOwnerContractUnavailable}, {[]string{uuid.NewString()}, ErrOwnerRelationshipConflict}, {[]string{first.String(), first.String()}, ErrOwnerRelationshipConflict}, {[]string{"not-a-uuid"}, ErrOwnerRelationshipConflict}, {[]string{strings.Repeat("x", 21000)}, ErrProjectionLimit},
	} {
		set(test.value)
		if _, err := call([]uuid.UUID{first, second}); !errors.Is(err, test.want) {
			t.Fatal("malformed/foreign provider response", err, test.want)
		}
	}
	set([]string{})
	if result, err := call(nil); err != nil || len(result) != 0 {
		t.Fatal("empty batch", result, err)
	}
	if _, err := call([]uuid.UUID{first, first}); !errors.Is(err, ErrTenantScope) {
		t.Fatal("duplicate input", err)
	}
	if _, err := call(make([]uuid.UUID, 501)); !errors.Is(err, ErrProjectionLimit) {
		t.Fatal("unbounded input", err)
	}
	stale := c
	stale.session.MembershipVersion++
	if err := mysqlstore.Snapshot(ctx, f.db, func(tx *sql.Tx) error { _, err := stale.Visible(ctx, tx, tenant, []uuid.UUID{first}); return err }); !errors.Is(err, ErrOwnerContractUnavailable) {
		t.Fatal("stale session", err)
	}
	if err := mysqlstore.Snapshot(ctx, f.db, func(tx *sql.Tx) error { _, err := c.Visible(ctx, tx, uuid.New(), []uuid.UUID{first}); return err }); !errors.Is(err, ErrDecisionAuthorizationDenied) {
		t.Fatal("foreign tenant", err)
	}
}
