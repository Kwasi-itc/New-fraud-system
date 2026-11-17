package sourceingest

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Rollback stages a NEW immutable generation from a retained publication and
// uses the normal parent fence/publication event. Old decisions are not edited.
func (s Store) Rollback(ctx context.Context, key Key, retained uuid.UUID) (Publication, error) {
	generation, err := s.StageRollback(ctx, key, retained)
	if err != nil {
		return Publication{}, err
	}
	return s.Publish(ctx, key, generation)
}

func (s Store) StageRollback(ctx context.Context, key Key, retained uuid.UUID) (uuid.UUID, error) {
	if err := s.checkKey(key); err != nil {
		return uuid.Nil, err
	}
	actor, err := s.publisher(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	generation := uuid.New()
	tag, err := tx.Exec(ctx, `INSERT INTO screening.source_versions(owner_scope,dataset_id,id,parent_generation,parent_revision,status,actor,content_hash,record_count,normalizer,source_key,parser_version)
 SELECT d.owner_scope,d.id,$3,d.active_generation,d.publication_revision,'staged',$4,v.content_hash,v.record_count,v.normalizer,v.source_key,v.parser_version
 FROM screening.source_datasets d JOIN screening.source_versions v ON (v.owner_scope,v.dataset_id)=(d.owner_scope,d.id)
 WHERE d.owner_scope=$1 AND d.id=$2 AND v.id=$5 AND v.status='published' AND (d.enabled OR $6)`, key.Owner, key.Dataset, generation, actor, retained, s.AllowDisabled && s.Scope != nil)
	if err != nil {
		return uuid.Nil, err
	}
	if tag.RowsAffected() != 1 {
		return uuid.Nil, sourcedata.ErrEvidence
	}
	if s.OnStaging != nil {
		if err = s.OnStaging(ctx, tx, generation); err != nil {
			return uuid.Nil, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_records SELECT owner_scope,dataset_id,$4,record_id,entity_id,entity_type,record FROM screening.source_records WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3`, key.Owner, key.Dataset, retained, generation); err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_names(owner_scope,dataset_id,generation,record_id,normalized,original) SELECT owner_scope,dataset_id,$4,record_id,normalized,original FROM screening.source_names WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3`, key.Owner, key.Dataset, retained, generation); err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_identifiers SELECT owner_scope,dataset_id,$4,record_id,type,normalized,country,issuer,original FROM screening.source_identifiers WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3`, key.Owner, key.Dataset, retained, generation); err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_version_artifacts SELECT owner_scope,dataset_id,$4,sha256 FROM screening.source_version_artifacts WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3`, key.Owner, key.Dataset, retained, generation); err != nil {
		return uuid.Nil, err
	}
	detail, _ := json.Marshal(map[string]any{"restored_generation": retained, "staged_generation": generation})
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_lifecycle_audit(owner_scope,dataset_id,action,actor,detail) VALUES($1,$2,'rollback-staged',$3,$4)`, key.Owner, key.Dataset, actor, detail); err != nil {
		return uuid.Nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return generation, nil
}

// RecoverInterrupted invalidates only expired staging leases. It never moves an
// active pointer and cannot race a live batch extending its lease under the row
// lock. Retrying the source creates a new fenced generation from retained bytes.
func (s Store) RecoverInterrupted(ctx context.Context, limit int) (int64, error) {
	if s.Scope != nil {
		return 0, sourcedata.ErrPublish
	}
	if _, err := s.publisher(ctx); err != nil {
		return 0, err
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	tag, err := s.DB.Exec(ctx, `WITH expired AS(SELECT owner_scope,dataset_id,id FROM screening.source_versions WHERE (NOT $2 OR owner_scope='00000000-0000-0000-0000-000000000000') AND status='staging' AND lease_until<=now() ORDER BY lease_until,id FOR UPDATE SKIP LOCKED LIMIT $1)
 UPDATE screening.source_versions v SET status='failed' FROM expired e WHERE (v.owner_scope,v.dataset_id,v.id)=(e.owner_scope,e.dataset_id,e.id)`, limit, s.GlobalOnly)
	return tag.RowsAffected(), err
}

// CleanupUnpublished intentionally never removes a published generation or its
// artifact: publication/audit/decision retention is indefinite until an approved
// evidence retention policy replaces that rule. Explicit per-dataset policies
// may expire only failed/staged projections. Metadata and audit remain durable.
func (s Store) CleanupUnpublished(ctx context.Context) (int, error) {
	if s.Scope != nil {
		return 0, sourcedata.ErrPublish
	}
	actor, err := s.publisher(ctx)
	if err != nil {
		return 0, err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var key Key
	var generation uuid.UUID
	err = tx.QueryRow(ctx, `SELECT v.owner_scope,v.dataset_id,v.id FROM screening.source_versions v JOIN screening.source_retention_policies p ON (p.owner_scope,p.dataset_id)=(v.owner_scope,v.dataset_id)
 WHERE (NOT $1 OR v.owner_scope='00000000-0000-0000-0000-000000000000') AND NOT p.legal_hold AND ((v.status='failed' AND v.created_at<now()-p.failed_after) OR(v.status='staged' AND v.created_at<now()-p.staged_after))
 AND NOT EXISTS(SELECT 1 FROM screening.source_publications pub WHERE (pub.owner_scope,pub.dataset_id,pub.generation)=(v.owner_scope,v.dataset_id,v.id))
 AND NOT EXISTS(SELECT 1 FROM screening.source_retention_holds h WHERE(h.owner_scope,h.dataset_id,h.generation)=(v.owner_scope,v.dataset_id,v.id))
 AND NOT EXISTS(SELECT 1 FROM screening.private_imports i WHERE(i.tenant_id,i.dataset_id,i.generation)=(v.owner_scope,v.dataset_id,v.id) AND i.status IN ('pending_review','published'))
 AND EXISTS(SELECT 1 FROM screening.source_records r WHERE (r.owner_scope,r.dataset_id,r.generation)=(v.owner_scope,v.dataset_id,v.id))
 ORDER BY v.created_at,v.id FOR NO KEY UPDATE OF v SKIP LOCKED LIMIT 1`, s.GlobalOnly).Scan(&key.Owner, &key.Dataset, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `UPDATE screening.source_versions SET status='failed' WHERE owner_scope=$1 AND dataset_id=$2 AND id=$3 AND status='staged'`, key.Owner, key.Dataset, generation); err != nil {
		return 0, err
	}
	// One bounded record page. Name count is bounded by validated aliases per row.
	rows, err := tx.Query(ctx, `SELECT record_id FROM screening.source_records WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3 ORDER BY record_id LIMIT 200`, key.Owner, key.Dataset, generation)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, table := range []string{"screening.source_identifiers", "screening.source_names", "screening.source_records"} {
		if _, err = tx.Exec(ctx, `DELETE FROM `+table+` WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3 AND record_id=ANY($4)`, key.Owner, key.Dataset, generation, ids); err != nil {
			return 0, err
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM screening.source_version_artifacts a WHERE a.owner_scope=$1 AND a.dataset_id=$2 AND a.generation=$3 AND NOT EXISTS(SELECT 1 FROM screening.source_records r WHERE (r.owner_scope,r.dataset_id,r.generation)=(a.owner_scope,a.dataset_id,a.generation))`, key.Owner, key.Dataset, generation); err != nil {
		return 0, err
	}

	detail, _ := json.Marshal(map[string]any{"generation": generation, "records": len(ids)})
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_lifecycle_audit(owner_scope,dataset_id,action,actor,detail) VALUES($1,$2,'cleanup-unpublished-page',$3,$4)`, key.Owner, key.Dataset, actor, detail); err != nil {
		return 0, err
	}
	return len(ids), tx.Commit(ctx)
}
