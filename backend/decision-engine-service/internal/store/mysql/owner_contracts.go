package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrOwnerContractUnavailable = errors.New("required decision cross-owner contract is unavailable")
var ErrOwnerRelationshipConflict = errors.New("cross-owner decision relationship is inconsistent")

// InboxContract consumes the approved case-owned SQL function. It does NOT
// implement case permissions or create the provider. Auth must first validate
// this trusted session and lock identity state before the provider's inbox locks.
// The provider rechecks tenant, current case permission, administrator/membership,
// inbox existence and optional active state. No external calls/COMMIT/DDL.
type InboxContract struct {
	schema  string
	session TrustedDecisionSession
}

func NewInboxContract(database string, session TrustedDecisionSession) (InboxContract, error) {
	if _, err := SessionAuthorizer(database, session); err != nil {
		return InboxContract{}, err
	}
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return InboxContract{}, err
	}
	return InboxContract{schema, session}, nil
}
func (c InboxContract) Authorize(ctx context.Context, tx *sql.Tx, tenant, inbox uuid.UUID, write, requireActive bool) error {
	if tx == nil || c.schema == "" {
		return ErrOwnerContractUnavailable
	}
	if tenant != c.session.TenantID || inbox == uuid.Nil || write && c.session.ReadOnly {
		return ErrDecisionAuthorizationDenied
	}
	var allowed mysqlstore.Bool
	if err := tx.QueryRowContext(ctx, "SELECT "+c.schema+".case_manager_authorize_decision_inbox_v1(?,?,?,?,?,?,?)", mysqlstore.UUID(c.session.ActorID), mysqlstore.UUID(tenant), mysqlstore.UUID(c.session.SessionID), c.session.MembershipVersion, mysqlstore.UUID(inbox), requireActive, write).Scan(&allowed); err != nil {
		return errors.Join(ErrOwnerContractUnavailable, err)
	}
	if !allowed {
		return ErrDecisionAuthorizationDenied
	}
	return nil
}

// ScreeningSource is a bounded owner publication snapshot. Version must advance
// with every status/evidence mutation. MatchCount and evidence are projected by
// screening under the same source lock; decision never scans provider history.
type ScreeningSource struct {
	ID, TenantID, DecisionID, ScenarioID, ConfigID uuid.UUID
	ExecutionID                                    mysqlstore.NullUUID
	Version                                        int64
	Status, ProviderReference, LastError           string
	Response                                       json.RawMessage
	UpdatedAt                                      mysqlstore.DateTime
	SentAt, CompletedAt, FailedAt                  mysqlstore.NullDateTime
}
type ScreeningSourceContract struct {
	schema   string
	maxBytes int
}

func NewScreeningSourceContract(database string, maxBytes int) (ScreeningSourceContract, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return ScreeningSourceContract{}, err
	}
	if maxBytes < 1 || maxBytes > 64<<20 {
		return ScreeningSourceContract{}, ErrProjectionLimit
	}
	return ScreeningSourceContract{schema, maxBytes}, nil
}

// Lock consumes a screening-owned, updatable MERGE read view using the caller's
// transaction. Provider must guarantee that FOR UPDATE locks exactly the source
// row; views which materialize/group/join-away that authority are incompatible.
// Lock order is source -> decision projection -> pending projection delivery.
// Missing provider/columns/rows fail closed; there is no guessed-table fallback.
func (c ScreeningSourceContract) Lock(ctx context.Context, tx *sql.Tx, tenant, source, expectedDecision, expectedConfig uuid.UUID) (ScreeningSource, error) {
	if tx == nil || c.schema == "" {
		return ScreeningSource{}, ErrOwnerContractUnavailable
	}
	if tenant == uuid.Nil || source == uuid.Nil || expectedDecision == uuid.Nil || expectedConfig == uuid.Nil {
		return ScreeningSource{}, ErrTenantScope
	}
	var out ScreeningSource
	var id, t, d, s, config mysqlstore.UUID
	var status, reference, lastError mysqlstore.Text
	var response mysqlstore.JSON
	var size int64
	query := "SELECT id,tenant_id,decision_id,scenario_id,config_id,execution_id,projection_version,status,updated_at,sent_at,completed_at,failed_at,OCTET_LENGTH(response_json)+OCTET_LENGTH(provider_reference)+OCTET_LENGTH(last_error),CASE WHEN OCTET_LENGTH(response_json)+OCTET_LENGTH(provider_reference)+OCTET_LENGTH(last_error)<=? THEN response_json ELSE 'null' END,CASE WHEN OCTET_LENGTH(provider_reference)<=? THEN provider_reference ELSE '' END,CASE WHEN OCTET_LENGTH(last_error)<=? THEN last_error ELSE '' END FROM " + c.schema + ".screening_decision_source_v1 WHERE tenant_id=? AND id=? FOR UPDATE"
	err := tx.QueryRowContext(ctx, query, c.maxBytes, c.maxBytes, c.maxBytes, mysqlstore.UUID(tenant), mysqlstore.UUID(source)).Scan(&id, &t, &d, &s, &config, &out.ExecutionID, &out.Version, &status, &out.UpdatedAt, &out.SentAt, &out.CompletedAt, &out.FailedAt, &size, &response, &reference, &lastError)
	if err != nil {
		return ScreeningSource{}, errors.Join(ErrOwnerContractUnavailable, err)
	}
	if size < 0 || size > int64(c.maxBytes) {
		return ScreeningSource{}, ErrProjectionLimit
	}
	out.ID, out.TenantID, out.DecisionID, out.ScenarioID, out.ConfigID = uuid.UUID(id), uuid.UUID(t), uuid.UUID(d), uuid.UUID(s), uuid.UUID(config)
	if out.ID != source || out.TenantID != tenant || out.DecisionID != expectedDecision || out.ConfigID != expectedConfig || out.ScenarioID == uuid.Nil || out.Version < 1 {
		return ScreeningSource{}, ErrOwnerRelationshipConflict
	}
	if out.ExecutionID.Valid && uuid.UUID(out.ExecutionID.UUID) == uuid.Nil {
		return ScreeningSource{}, ErrOwnerRelationshipConflict
	}
	out.Status, out.ProviderReference, out.LastError = string(status), string(reference), string(lastError)
	out.Response = append([]byte(nil), response...)
	switch out.Status {
	case "pending", "queued", "processing", "awaiting_review", "confirmed_hit", "no_hit", "archived", "failed":
	default:
		return ScreeningSource{}, ErrOwnerRelationshipConflict
	}
	return out, nil
}
