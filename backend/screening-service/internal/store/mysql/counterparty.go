package mysql

import (
	"context"
	"database/sql"
	"errors"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlpublication"
	"github.com/google/uuid"
	"sort"
	"strings"
)

// CounterpartyScope follows the immutable applied-refinement receipt. A linked
// manual preview inherits only the original decision's whitelist scope; it
// remains an independent manual search in the stored evidence.
func (s *Store) CounterpartyScope(ctx context.Context, parent screening.Screening) (screening.Screening, error) {
	if !parent.IsManual {
		return parent, nil
	}
	var root string
	err := s.q.QueryRowContext(ctx, "SELECT root_id FROM "+s.table("refinements")+" WHERE tenant_id=? AND screening_id=? AND applied_at IS NOT NULL", id(parent.TenantID), id(parent.ID)).Scan(uid(&root))
	if errors.Is(err, sql.ErrNoRows) {
		return parent, nil
	}
	if err != nil {
		return parent, err
	}
	original, err := s.Screenings().GetByID(ctx, parent.TenantID, root)
	if err != nil {
		return parent, err
	}
	if original.IsManual || original.DecisionID == "" || original.Provider != parent.Provider {
		return parent, identity.Invalid("Original counterparty scope is unavailable.")
	}
	parent.UniqueCounterpartyIdentifier = original.UniqueCounterpartyIdentifier
	parent.DecisionID = original.DecisionID
	parent.IsManual = false
	return parent, nil
}

func (s *Store) CounterpartyExclusions(ctx context.Context, tenant, provider, counterparty string, entities []string) (map[string]string, error) {
	t, e := uuid.Parse(tenant)
	if e != nil {
		return nil, e
	}
	return mysqlpublication.ActiveCounterpartyExclusions(ctx, s.q, strings.Trim(s.schema, "`"), t, provider, counterparty, entities, true)
}

func (s *Store) RecordCounterpartyApplications(ctx context.Context, tenant, screeningID string, applications map[string]string) error {
	if len(applications) == 0 {
		return nil
	}
	if len(applications) > 500 {
		return ErrLimit
	}
	keys := []string{}
	for match := range applications {
		keys = append(keys, match)
	}
	sort.Strings(keys)
	values := []string{}
	args := []any{}
	for _, match := range keys {
		values = append(values, "(?,?,?,?,UTC_TIMESTAMP(6))")
		args = append(args, id(tenant), id(screeningID), id(matchKey(match)), id(applications[match]))
	}
	_, e := s.q.ExecContext(ctx, "INSERT INTO "+s.table("counterparty_applications")+"(tenant_id,screening_id,match_id,operation_id,created_at) VALUES"+strings.Join(values, ","), args...)
	return normalize(e)
}
func (s *Store) SetCounterpartyExclusion(ctx context.Context, parent screening.Screening, match screening.Match, actor string, command *identity.Command, active bool) error {
	if parent.UniqueCounterpartyIdentifier == nil || *parent.UniqueCounterpartyIdentifier == "" || parent.IsManual || parent.DecisionID == "" {
		return identity.Invalid("A mapped counterparty identifier is required.")
	}
	if _, e := s.q.ExecContext(ctx, "INSERT INTO "+s.table("counterparty_exclusion_events")+"(tenant_id,operation_id,provider,counterparty_id,entity_id,active,actor_id,findings,created_at) VALUES(?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6))", id(parent.TenantID), id(command.OperationID.String()), []byte(parent.Provider), []byte(*parent.UniqueCounterpartyIdentifier), []byte(match.EntityID), active, id(actor), command.Findings); e != nil {
		return normalize(e)
	}
	_, e := s.q.ExecContext(ctx, "INSERT INTO "+s.table("counterparty_exclusions")+"(tenant_id,provider,counterparty_id,entity_id,active,operation_id,actor_id,updated_at) VALUES(?,?,?,?,?,?,?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE active=VALUES(active),operation_id=VALUES(operation_id),actor_id=VALUES(actor_id),updated_at=VALUES(updated_at)", id(parent.TenantID), []byte(parent.Provider), []byte(*parent.UniqueCounterpartyIdentifier), []byte(match.EntityID), active, id(command.OperationID.String()), id(actor))
	return normalize(e)
}
