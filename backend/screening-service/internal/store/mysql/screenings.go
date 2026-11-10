package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	d "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

const screeningColumns = `id,tenant_id,decision_id,scenario_id,screening_config_id,COALESCE(idempotency_key,''),provider,object_type,object_id,status,request_json,response_json,provider_reference,last_error,is_manual,is_archived,partial,unique_counterparty_identifier,created_at,updated_at,sent_at,completed_at,failed_at`

func scanScreening(row scanner) (d.Screening, error) {
	var v d.Screening
	e := row.Scan(uid(&v.ID), uid(&v.TenantID), uid(&v.DecisionID), uid(&v.ScenarioID), uid(&v.ScreeningConfigID), &v.IdempotencyKey, &v.Provider, &v.ObjectType, &v.ObjectID, &v.Status, &v.RequestJSON, &v.ResponseJSON, &v.ProviderReference, &v.LastError, &v.IsManual, &v.IsArchived, &v.Partial, &v.UniqueCounterpartyIdentifier, &v.CreatedAt, &v.UpdatedAt, &v.SentAt, &v.CompletedAt, &v.FailedAt)
	return v, normalize(e)
}
func (r screenings) GetByID(ctx context.Context, t, s string) (d.Screening, error) {
	return scanScreening(r.q.QueryRowContext(ctx, "SELECT "+screeningColumns+" FROM "+r.table("screenings")+" WHERE tenant_id=? AND id=?", id(t), id(s)))
}
func (r screenings) GetByIdempotencyKey(ctx context.Context, t, key string) (d.Screening, error) {
	if _, transactional := r.q.(*sql.Tx); transactional {
		if len(key) == 0 || len(key) > 512 {
			return d.Screening{}, errors.New("invalid request key")
		}
		if _, e := r.q.ExecContext(ctx, "INSERT INTO "+r.table("request_keys")+"(tenant_id,request_key) VALUES(?,?) ON DUPLICATE KEY UPDATE request_key=request_key", id(t), []byte(key)); e != nil {
			return d.Screening{}, normalize(e)
		}
	}
	return scanScreening(r.q.QueryRowContext(ctx, "SELECT "+screeningColumns+" FROM "+r.table("screenings")+" WHERE tenant_id=? AND idempotency_key=?", id(t), []byte(key)))
}
func (r screenings) Create(ctx context.Context, v d.Screening) (d.Screening, error) {
	var execution any
	if v.DecisionID != "" {
		var found string
		e := r.q.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_screening_executions WHERE tenant_id=? AND decision_id=? AND scenario_id=? AND config_id=? AND id=?", id(v.TenantID), id(v.DecisionID), id(v.ScenarioID), id(v.ScreeningConfigID), id(v.IdempotencyKey)).Scan(uid(&found))
		if e != nil {
			return d.Screening{}, normalize(e)
		}
		execution = id(found)
	}
	var key any
	if v.IdempotencyKey != "" {
		key = []byte(v.IdempotencyKey)
	}
	_, e := r.q.ExecContext(ctx, "INSERT INTO "+r.table("screenings")+`(id,tenant_id,decision_id,scenario_id,screening_config_id,execution_id,idempotency_key,provider,object_type,object_id,status,request_json,response_json,decision_projection,provider_reference,last_error,is_manual,is_archived,partial,unique_counterparty_identifier,created_at,updated_at,sent_at,completed_at,failed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,JSON_OBJECT(),?,?,?,?,?,?,?,?,?,?,?)`, id(v.ID), id(v.TenantID), nullableID(v.DecisionID), nullableID(v.ScenarioID), nullableID(v.ScreeningConfigID), execution, key, v.Provider, v.ObjectType, v.ObjectID, v.Status, jsonValue(v.RequestJSON), jsonValue(v.ResponseJSON), v.ProviderReference, v.LastError, v.IsManual, v.IsArchived, v.Partial, v.UniqueCounterpartyIdentifier, v.CreatedAt, v.UpdatedAt, v.SentAt, v.CompletedAt, v.FailedAt)
	if e != nil {
		return d.Screening{}, normalize(e)
	}
	return r.GetByID(ctx, v.TenantID, v.ID)
}
func (r screenings) SetManualCreator(ctx context.Context, t, s, actor string) error {
	result, e := r.q.ExecContext(ctx, "UPDATE "+r.table("screenings")+" SET creator_id=?,projection_version=projection_version+1 WHERE tenant_id=? AND id=? AND is_manual AND decision_id IS NULL AND(creator_id IS NULL OR creator_id=?)", id(actor), id(t), id(s), id(actor))
	if e != nil {
		return normalize(e)
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return errors.New("manual ownership conflict")
	}
	return nil
}
func (r screenings) Update(ctx context.Context, v d.Screening) (d.Screening, error) {
	result, e := r.q.ExecContext(ctx, "UPDATE "+r.table("screenings")+` SET status=?,request_json=?,response_json=?,provider_reference=?,last_error=?,is_manual=?,is_archived=?,partial=?,unique_counterparty_identifier=?,updated_at=?,sent_at=?,completed_at=?,failed_at=?,match_count=(SELECT COUNT(*) FROM `+r.table("matches")+` m WHERE m.tenant_id=? AND m.screening_id=?),projection_version=projection_version+1,processing_token=IF(?='processing',processing_token,NULL),processing_until=IF(?='processing',processing_until,NULL) WHERE tenant_id=? AND id=?`, v.Status, jsonValue(v.RequestJSON), jsonValue(v.ResponseJSON), v.ProviderReference, v.LastError, v.IsManual, v.IsArchived, v.Partial, v.UniqueCounterpartyIdentifier, v.UpdatedAt, v.SentAt, v.CompletedAt, v.FailedAt, id(v.TenantID), id(v.ID), v.Status, v.Status, id(v.TenantID), id(v.ID))
	if e != nil {
		return d.Screening{}, normalize(e)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return d.Screening{}, pgx.ErrNoRows
	}
	return r.GetByID(ctx, v.TenantID, v.ID)
}
func (r screenings) ListByDecision(ctx context.Context, t, decision string) ([]d.Screening, error) {
	return list(ctx, r.Store, "SELECT "+screeningColumns+" FROM "+r.table("screenings")+" WHERE tenant_id=? AND decision_id=? ORDER BY created_at,id LIMIT 501", []any{id(t), id(decision)}, 500, scanScreening)
}
func (r screenings) ListByStatus(ctx context.Context, status d.Status, n int) ([]d.Screening, error) {
	return list(ctx, r.Store, "SELECT "+screeningColumns+" FROM "+r.table("screenings")+" WHERE status=? AND provider<>'local' AND EXISTS(SELECT 1 FROM "+r.schema+".identity_tenants t WHERE t.id="+r.table("screenings")+".tenant_id AND t.active) ORDER BY created_at,id LIMIT ?", []any{status, batch(n)}, 500, scanScreening)
}
func (r screenings) ListRecoverable(ctx context.Context, now time.Time, n int) ([]d.Screening, error) {
	return list(ctx, r.Store, "SELECT "+screeningColumns+" FROM "+r.table("screenings")+" WHERE status='processing' AND provider<>'local' AND EXISTS(SELECT 1 FROM "+r.schema+".identity_tenants t WHERE t.id="+r.table("screenings")+".tenant_id AND t.active) AND(processing_until IS NULL OR processing_until<=?) ORDER BY processing_until,id LIMIT ?", []any{now, batch(n)}, 500, scanScreening)
}
func (r screenings) Lock(ctx context.Context, t, s string) (d.Screening, error) {
	return scanScreening(r.q.QueryRowContext(ctx, "SELECT "+screeningColumns+" FROM "+r.table("screenings")+" WHERE tenant_id=? AND id=? FOR UPDATE", id(t), id(s)))
}
func (r screenings) Claim(ctx context.Context, t, s, token string, now time.Time) (bool, error) {
	result, e := r.q.ExecContext(ctx, "UPDATE "+r.table("screenings")+" SET status='processing',processing_token=?,processing_until=TIMESTAMPADD(MINUTE,6,?),sent_at=?,updated_at=?,projection_version=projection_version+1 WHERE tenant_id=? AND id=? AND provider<>'local' AND EXISTS(SELECT 1 FROM "+r.schema+".identity_tenants t WHERE t.id="+r.table("screenings")+".tenant_id AND t.active) AND(status='pending' OR(status='processing' AND(processing_until IS NULL OR processing_until<=?)))", id(token), now, now, now, id(t), id(s), now)
	if e != nil {
		return false, normalize(e)
	}
	n, e := result.RowsAffected()
	return n == 1, e
}
func (r screenings) LockClaim(ctx context.Context, t, s, token string) error {
	var found string
	e := r.q.QueryRowContext(ctx, "SELECT id FROM "+r.table("screenings")+" WHERE tenant_id=? AND id=? AND status='processing' AND processing_token=? AND processing_until>UTC_TIMESTAMP(6) FOR UPDATE", id(t), id(s), id(token)).Scan(uid(&found))
	if errors.Is(normalize(e), pgx.ErrNoRows) {
		return ports.ErrScreeningBusy
	}
	return normalize(e)
}
func (r screenings) ReleaseClaim(ctx context.Context, t, s, token string) error {
	_, e := r.q.ExecContext(ctx, "UPDATE "+r.table("screenings")+" SET status='pending',processing_token=NULL,processing_until=NULL,projection_version=projection_version+1 WHERE tenant_id=? AND id=? AND status='processing' AND processing_token=?", id(t), id(s), id(token))
	return normalize(e)
}
func (r screenings) CreateContinuousRun(ctx context.Context, t, s, c, m string, now time.Time) error {
	_, e := r.q.ExecContext(ctx, "INSERT INTO "+r.table("continuous_runs")+"(tenant_id,screening_id,config_id,monitored_object_id,created_at) VALUES(?,?,?,?,?)", id(t), id(s), id(c), id(m), now)
	return normalize(e)
}
func (r screenings) IsContinuousRun(ctx context.Context, t, s string) (bool, error) {
	var found bool
	e := r.q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+r.table("continuous_runs")+" WHERE tenant_id=? AND screening_id=?)", id(t), id(s)).Scan(&found)
	return found, normalize(e)
}
func (r screenings) ListManualHistory(ctx context.Context, t, actor string, f d.ManualHistoryFilter) ([]d.ManualHistoryItem, error) {
	if f.Limit < 1 || f.Limit > 100 {
		return nil, errors.New("invalid history limit")
	}
	q := "SELECT id,request_json,object_type,status,partial,created_at FROM " + r.table("screenings") + " WHERE tenant_id=? AND creator_id=? AND is_manual AND decision_id IS NULL"
	a := []any{id(t), id(actor)}
	if f.Status != "" {
		q += " AND status=?"
		a = append(a, f.Status)
	}
	if f.EntityType != "" {
		q += " AND object_type=?"
		a = append(a, f.EntityType)
	}
	if f.From != nil {
		q += " AND created_at>=?"
		a = append(a, *f.From)
	}
	if f.Until != nil {
		q += " AND created_at<?"
		a = append(a, *f.Until)
	}
	if f.Before != nil {
		q += " AND(created_at<? OR(created_at=? AND id<?))"
		a = append(a, *f.Before, *f.Before, id(f.BeforeID))
	}
	q += " ORDER BY created_at DESC,id DESC LIMIT ?"
	a = append(a, f.Limit+1)
	return list(ctx, r.Store, q, a, f.Limit+1, func(row scanner) (d.ManualHistoryItem, error) {
		var v d.ManualHistoryItem
		var raw []byte
		e := row.Scan(uid(&v.ID), &raw, &v.EntityType, &v.Status, &v.Partial, &v.CreatedAt)
		var req d.SearchRequest
		if e == nil {
			e = json.Unmarshal(raw, &req)
			if len(req.Queries) > 0 {
				v.Name = strings.TrimSpace(req.Queries[0].Name)
			}
		}
		return v, normalize(e)
	})
}
