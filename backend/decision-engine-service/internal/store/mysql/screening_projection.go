package mysql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// ReconcileSource locks the provider publication before this decision-owned
// projection. Only the owner read contract is accessed; it never updates
// screening tables or acknowledges delivery on the owner's behalf.
func (r ScreeningExecutionRepository) ReconcileSource(ctx context.Context, tenant, source, decision, config string) (screening.Execution, error) {
	if err := r.mutating(); err != nil {
		return screening.Execution{}, err
	}
	ids, err := r.ids(tenant, source, decision, config)
	if err != nil {
		return screening.Execution{}, err
	}
	if r.source == nil {
		return screening.Execution{}, ErrOwnerContractUnavailable
	}
	s, err := r.source.Lock(ctx, r.tx, r.tenant, uuid.UUID(ids[1].(mysqlstore.UUID)), uuid.UUID(ids[2].(mysqlstore.UUID)), uuid.UUID(ids[3].(mysqlstore.UUID)))
	if err != nil {
		return screening.Execution{}, err
	}
	where := "tenant_id=? AND decision_id=? AND config_id=?"
	args := []any{ids[0], ids[2], ids[3]}
	if s.ExecutionID.Valid {
		where += " AND id=?"
		args = append(args, s.ExecutionID.UUID)
	}
	rows, err := r.tx.QueryContext(ctx, "SELECT id,projection_source_id,projection_source_version FROM "+r.schema+".decision_engine_screening_executions WHERE "+where+" ORDER BY id LIMIT 2 FOR UPDATE", args...)
	if err != nil {
		return screening.Execution{}, err
	}
	var candidates int
	var id mysqlstore.UUID
	var linked mysqlstore.NullUUID
	var version int64
	for rows.Next() {
		candidates++
		if err := rows.Scan(&id, &linked, &version); err != nil {
			rows.Close()
			return screening.Execution{}, err
		}
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return screening.Execution{}, err
	}
	if candidates != 1 || linked.Valid && uuid.UUID(linked.UUID) != s.ID {
		return screening.Execution{}, ErrOwnerRelationshipConflict
	}
	v, err := r.get(ctx, tenant, uuid.UUID(id).String())
	if err != nil {
		return screening.Execution{}, err
	}
	if v.scenario != s.ScenarioID.String() {
		return screening.Execution{}, ErrOwnerRelationshipConflict
	}
	if linked.Valid && version >= s.Version {
		return screeningExecution(v), nil
	}
	next := "pending"
	switch s.Status {
	case "processing":
		next = "sent"
	case "awaiting_review", "confirmed_hit", "no_hit", "archived":
		next = "completed"
	case "failed":
		next = "failed"
	}
	var response map[string]any
	decoder := json.NewDecoder(bytes.NewReader(s.Response))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil || response == nil {
		return screening.Execution{}, ErrOwnerRelationshipConflict
	}
	response["screening_id"] = s.ID.String()
	response["decision_id"] = s.DecisionID.String()
	response["scenario_id"] = s.ScenarioID.String()
	response["screening_config_id"] = s.ConfigID.String()
	response["status"] = s.Status
	response["source_version"] = s.Version
	raw, err := json.Marshal(response)
	if err != nil {
		return screening.Execution{}, err
	}
	if len(raw)+len(s.ProviderReference)+len(s.LastError) > r.maxBytes {
		return screening.Execution{}, ErrProjectionLimit
	}
	// Retain original send/completion times when a later publication omits them.
	sent, completed, failed := s.SentAt, s.CompletedAt, s.FailedAt
	if !sent.Valid && v.sent != nil {
		sent, err = optionalDate(v.sent)
		if err != nil {
			return screening.Execution{}, err
		}
	}
	if !completed.Valid && v.completed != nil {
		completed, err = optionalDate(v.completed)
		if err != nil {
			return screening.Execution{}, err
		}
	}
	if !failed.Valid && v.failed != nil {
		failed, err = optionalDate(v.failed)
		if err != nil {
			return screening.Execution{}, err
		}
	}
	if next == "sent" && !sent.Valid {
		sent = mysqlstore.NullDateTime{DateTime: s.UpdatedAt, Valid: true}
	}
	if next == "completed" && !completed.Valid {
		completed = mysqlstore.NullDateTime{DateTime: s.UpdatedAt, Valid: true}
	}
	if next == "failed" && !failed.Valid {
		failed = mysqlstore.NullDateTime{DateTime: s.UpdatedAt, Valid: true}
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_screening_executions SET status=?,response_json=?,provider_reference=?,last_error=?,updated_at=?,sent_at=?,completed_at=?,failed_at=?,projection_source_id=?,projection_source_version=? WHERE tenant_id=? AND id=?", next, mysqlstore.JSON(raw), mysqlstore.Text(s.ProviderReference), mysqlstore.Text(s.LastError), s.UpdatedAt, sent, completed, failed, mysqlstore.UUID(s.ID), s.Version, ids[0], id)
	if err != nil {
		return screening.Execution{}, err
	}
	return r.GetByID(ctx, tenant, uuid.UUID(id).String())
}
