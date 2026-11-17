// Package sourcemonitor owns explicit query-snapshot monitoring. It shares the
// screening queue and immutable results; publication delivery has its own bounded
// durable cursor and receipts rather than depending on a provider delta page.
package sourcemonitor

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/riverjobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	DB               *pgxpool.Pool
	Enqueue          riverjobs.ScreeningEnqueuer
	MonitoredEnqueue riverjobs.MonitoredObjectEnqueuer
}

// Enroll requires the same signed current creator identity as manual history.
// No operator token, tenant field or request-supplied query grants authority.
func (s Service) Enroll(ctx context.Context, p identity.Principal, origin uuid.UUID) (uuid.UUID, error) {
	scope, err := provideraccess.FromPrincipal(p)
	if err != nil {
		return uuid.Nil, err
	}
	if err = identity.CheckIdentity(ctx, s.DB, p, false); err != nil {
		return uuid.Nil, err
	}
	var raw []byte
	err = s.DB.QueryRow(ctx, `SELECT request_json FROM screening.screenings WHERE tenant_id=$1 AND id=$2 AND provider=$3 AND is_manual AND idempotency_key LIKE 'manual:'||$4||':%'`, p.TenantID, origin, sourcedata.Provider, p.Subject).Scan(&raw)
	if err != nil {
		return uuid.Nil, provideraccess.ErrScope
	}
	var request screening.SearchRequest
	if json.Unmarshal(raw, &request) != nil {
		return uuid.Nil, sourcedata.ErrInput
	}
	if err = (internalprovider.Provider{DB: s.DB}).Validate(ctx, scope, request); err != nil {
		return uuid.Nil, err
	}
	selection, _ := sourcedata.DecodeSelection(request.ProviderConfig)
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	if err = identity.CheckIdentity(ctx, tx, p, true); err != nil {
		return uuid.Nil, err
	}
	id := uuid.New()
	err = tx.QueryRow(ctx, `INSERT INTO screening.source_monitors(id,tenant_id,origin_screening_id,latest_screening_id,created_by) VALUES($1,$2,$3,$3,$4) ON CONFLICT(tenant_id,origin_screening_id,created_by) DO UPDATE SET enabled=true,updated_at=now() RETURNING id`, id, p.TenantID, origin, p.Subject).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	for _, ref := range selection.Datasets {
		owner := uuid.Nil
		if ref.Scope == "tenant" {
			owner = p.TenantID
		}
		if _, err = tx.Exec(ctx, `INSERT INTO screening.source_monitor_datasets(monitor_id,tenant_id,owner_scope,dataset_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, id, p.TenantID, owner, ref.ID); err != nil {
			return uuid.Nil, err
		}
	}
	// Enrollment reconciles the current generation even when publication fanout
	// passed this UUID before enrollment; no new subscription misses the snapshot.
	_, err = tx.Exec(ctx, `INSERT INTO screening.source_rescreen_targets(id,publication_id,tenant_id,monitor_id) SELECT gen_random_uuid(),p.id,$2,$1 FROM screening.source_monitor_datasets d JOIN screening.source_datasets ds ON (ds.owner_scope,ds.id)=(d.owner_scope,d.dataset_id) JOIN screening.source_publications p ON (p.owner_scope,p.dataset_id,p.generation)=(ds.owner_scope,ds.id,ds.active_generation) WHERE d.monitor_id=$1 ON CONFLICT(publication_id,monitor_id) DO NOTHING`, id, p.TenantID)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_monitor_audit(tenant_id,monitor_id,actor,action) VALUES($1,$2,$3,'enabled')`, p.TenantID, id, p.Subject); err != nil {
		return uuid.Nil, err
	}
	return id, tx.Commit(ctx)
}

// FanoutPage atomically saves at most 200 target receipts and the associated
// continuation. A crash before commit repeats the page; duplicates are unique.
func (s Service) FanoutPage(ctx context.Context) (bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var publication int64
	var after uuid.UUID
	err = tx.QueryRow(ctx, `SELECT publication_id,after_monitor FROM screening.source_publication_delivery WHERE state='pending' ORDER BY publication_id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&publication, &after)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	rows, err := tx.Query(ctx, `SELECT m.id,m.tenant_id FROM screening.source_publications p JOIN screening.source_monitor_datasets d ON (d.owner_scope,d.dataset_id)=(p.owner_scope,p.dataset_id) JOIN screening.source_monitors m ON (m.tenant_id,m.id)=(d.tenant_id,d.monitor_id) WHERE p.id=$1 AND m.id>$2 AND m.enabled ORDER BY m.id LIMIT 200`, publication, after)
	if err != nil {
		return false, err
	}
	type target struct{ id, tenant uuid.UUID }
	items := []target{}
	for rows.Next() {
		var item target
		if err = rows.Scan(&item.id, &item.tenant); err != nil {
			rows.Close()
			return false, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if _, err = tx.Exec(ctx, `INSERT INTO screening.source_rescreen_targets(id,publication_id,tenant_id,monitor_id) VALUES($1,$2,$3,$4) ON CONFLICT(publication_id,monitor_id) DO NOTHING`, uuid.New(), publication, item.tenant, item.id); err != nil {
			return false, err
		}
		after = item.id
	}
	state := "pending"
	if len(items) < 200 {
		state = "completed"
	}
	_, err = tx.Exec(ctx, `UPDATE screening.source_publication_delivery SET after_monitor=$2,state=$3,processed=processed+$4,updated_at=now() WHERE publication_id=$1`, publication, after, state, len(items))
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// QueueOne uses a durable target as institutional authority. It never borrows a
// human login session; current membership, tenant and dataset grants are checked
// again by the provider at execution. Queue acceptance is not run completion.
func (s Service) QueueOne(ctx context.Context) (bool, error) {
	if s.Enqueue == nil {
		return false, errors.New("source_monitor_queue_unavailable")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var id, tenant, monitor uuid.UUID
	var raw []byte
	var enabled bool
	err = tx.QueryRow(ctx, `SELECT t.id,t.tenant_id,t.monitor_id,o.request_json,m.enabled FROM screening.source_rescreen_targets t JOIN screening.source_monitors m ON (m.tenant_id,m.id)=(t.tenant_id,t.monitor_id) JOIN screening.screenings o ON (o.tenant_id,o.id)=(m.tenant_id,m.origin_screening_id) WHERE t.status='pending' AND t.next_attempt_at<=now() ORDER BY t.next_attempt_at,t.id FOR UPDATE OF t SKIP LOCKED LIMIT 1`).Scan(&id, &tenant, &monitor, &raw, &enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !enabled {
		_, err = tx.Exec(ctx, `UPDATE screening.source_rescreen_targets SET status='cancelled',last_error='monitor_disabled',updated_at=now() WHERE id=$1`, id)
		if err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}
	var request screening.SearchRequest
	if json.Unmarshal(raw, &request) != nil {
		return false, sourcedata.ErrInput
	}
	screenID := uuid.New()
	request.IdempotencyKey = "source-rescreen:" + id.String() + ":" + screenID.String()
	request.IsManual = false
	request.DecisionID = ""
	request.ScenarioID = ""
	request.ScreeningConfigID = ""
	raw, _ = json.Marshal(request)
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO screening.screenings(id,tenant_id,provider,object_type,object_id,status,request_json,response_json,is_manual,idempotency_key,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'pending',$6,'{}',false,$7,$8,$8)`, screenID, tenant, request.Provider, request.ObjectType, request.ObjectID, raw, request.IdempotencyKey, now)
	if err != nil {
		return false, err
	}
	authority, _ := json.Marshal(map[string]any{"kind": "source-monitor", "target_id": id})
	if _, err = tx.Exec(ctx, `INSERT INTO screening.provider_run_authority(screening_id,tenant_id,authority) VALUES($1,$2,$3)`, screenID, tenant, authority); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE screening.source_rescreen_targets SET status='queued',screening_id=$2,attempts=attempts+1,updated_at=now() WHERE id=$1`, id, screenID); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_rescreen_attempts(target_id,tenant_id,screening_id) VALUES($1,$2,$3)`, id, tenant, screenID); err != nil {
		return false, err
	}
	if err = s.Enqueue.EnqueueTx(ctx, tx, tenant.String(), screenID.String(), nil); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s Service) RunBatch(ctx context.Context, limit int) error {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	for i := 0; i < limit; i++ {
		more, e := s.FanoutContinuousPage(ctx)
		if e != nil {
			return e
		}
		if !more {
			break
		}
	}
	// Bounded automatic retry. Old attempts/results remain immutable. Exhaustion
	// remains failed/stale and visible until an authorized explicit retry.
	if _, err := s.DB.Exec(ctx, `WITH due AS (SELECT id FROM screening.source_rescreen_targets WHERE status='failed' AND attempts<5 AND next_attempt_at<=now() ORDER BY next_attempt_at,id FOR UPDATE SKIP LOCKED LIMIT $1) UPDATE screening.source_rescreen_targets t SET status='pending',updated_at=now() FROM due WHERE t.id=due.id`, limit); err != nil {
		return err
	}
	for i := 0; i < limit; i++ {
		more, err := s.FanoutPage(ctx)
		if err != nil {
			return err
		}
		if !more {
			break
		}
	}
	for i := 0; i < limit; i++ {
		more, err := s.QueueOne(ctx)
		if err != nil {
			return err
		}
		if !more {
			break
		}
	}
	return nil
}
