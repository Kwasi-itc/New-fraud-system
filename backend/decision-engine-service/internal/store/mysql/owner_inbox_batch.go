package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// AuthorizeBatch consumes the approved case-owned batch write/read contract.
// The provider must check current trusted identity, tenant, permissions and
// optional active status for EVERY requested inbox, locking in UUID order for
// writes. It runs locally on this caller transaction after identity authority
// and before decision owner locks. No case mutations, network IO, DDL or COMMIT
// are permitted. A missing/NULL provider fails closed, including an empty batch.
// This consumer does not install or substitute for the case owner's provider.
func (c InboxContract) AuthorizeBatch(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, ids []uuid.UUID, write, requireActive bool) error {
	if tx == nil || c.schema == "" {
		return ErrOwnerContractUnavailable
	}
	if tenant != c.session.TenantID || write && c.session.ReadOnly {
		return ErrDecisionAuthorizationDenied
	}
	if len(ids) > 500 {
		return ErrProjectionLimit
	}
	ordered := append([]uuid.UUID(nil), ids...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
	values := make([]string, len(ordered))
	for i, id := range ordered {
		if id == uuid.Nil || i > 0 && ordered[i-1] == id {
			return ErrTenantScope
		}
		values[i] = id.String()
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return err
	}
	var allowed mysqlstore.Bool
	err = tx.QueryRowContext(ctx, "SELECT "+c.schema+".case_manager_authorize_decision_inboxes_v1(?,?,?,?,?,?,?)", mysqlstore.UUID(c.session.ActorID), mysqlstore.UUID(tenant), mysqlstore.UUID(c.session.SessionID), c.session.MembershipVersion, mysqlstore.JSON(raw), requireActive, write).Scan(&allowed)
	if err != nil {
		return errors.Join(ErrOwnerContractUnavailable, err)
	}
	if !allowed {
		return ErrDecisionAuthorizationDenied
	}
	return nil
}
