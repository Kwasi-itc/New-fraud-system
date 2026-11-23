package sourceingest

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PublishSnapshot accepts an explicitly referenced local JSON snapshot of an
// already approved global source. Acceptance is recorded as local snapshot
// intake, not a successful network fetch or an independent source-rights review.
// Collection is paused and fenced; other datasets and existing approvals are
// untouched. Staging/validation and file IO finish before the publication lock.
func (s Store) PublishSnapshot(ctx context.Context, key Key, generation uuid.UUID, reference string) (Publication, error) {
	if key.Owner != uuid.Nil || s.Scope != nil || s.Guard != nil || s.Finalize != nil || strings.TrimSpace(reference) == "" || len(reference) > 500 {
		return Publication{}, sourcedata.ErrPublish
	}
	actor, err := s.publisher(ctx)
	if err != nil {
		return Publication{}, err
	}
	run := uuid.New()
	var artifact string
	var count int
	var fence int64
	guard := func(ctx context.Context, tx pgx.Tx) error {
		var approved, compatible bool
		err := tx.QueryRow(ctx, `SELECT c.rights_state='approved',
 c.connector=v.source_key AND c.config->>'parser'=v.parser_version AND v.parser_version<>'controlled-json-v1'
 AND (c.enabled OR c.config->>'intake_mode'='reviewed_snapshot'),c.fence,a.sha256,v.record_count
 FROM screening.source_connections c
 JOIN screening.source_versions v ON(v.owner_scope,v.dataset_id,v.id)=(c.owner_scope,c.dataset_id,$3)
 JOIN screening.source_version_artifacts a ON(a.owner_scope,a.dataset_id,a.generation)=(v.owner_scope,v.dataset_id,v.id)
 JOIN screening.source_artifacts raw ON(raw.owner_scope,raw.dataset_id,raw.sha256)=(a.owner_scope,a.dataset_id,a.sha256)
 WHERE c.owner_scope=$1 AND c.dataset_id=$2 AND v.status IN('staged','published') AND raw.media_type='application/json'
 FOR NO KEY UPDATE OF c`, key.Owner, key.Dataset, generation).Scan(&approved, &compatible, &fence, &artifact, &count)
		if err != nil || !approved || !compatible || count < 1 {
			if err != nil && err != pgx.ErrNoRows {
				return err
			}
			return sourcedata.ErrPublish
		}
		return nil
	}
	s.Finalize = func(ctx context.Context, tx pgx.Tx, result Publication) error {
		detail, err := json.Marshal(map[string]any{"kind": "reviewed_snapshot", "reference": reference, "staged_generation": generation, "published_generation": result.Generation, "artifact_sha256": artifact, "record_count": count, "external_fetch": false})
		if err != nil {
			return err
		}
		// Readiness dates the explicit local intake. It does not claim that the
		// publisher website was checked; the receipt and config retain that fact.
		if _, err = tx.Exec(ctx, `UPDATE screening.source_connections SET enabled=false,
 fence=fence+1,lease_token=NULL,lease_until=NULL,last_attempt_at=now(),last_success_at=now(),
 verified_generation=$3,last_error='',failures=0,
 config=config||jsonb_build_object('intake_mode','reviewed_snapshot','snapshot_intake',$4::jsonb)
 WHERE owner_scope=$1 AND dataset_id=$2`, key.Owner, key.Dataset, result.Generation, detail); err != nil {
			return err
		}
		status := "published"
		if result.Unchanged {
			status = "unchanged"
		}
		if _, err = tx.Exec(ctx, `INSERT INTO screening.source_import_runs(id,owner_scope,dataset_id,fence,generation,artifact_sha256,status,diagnostic,record_count,completed_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,now())`, run, key.Owner, key.Dataset, fence+1, result.Generation, artifact, status, "reviewed_snapshot: "+reference, count); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO screening.source_lifecycle_audit(owner_scope,dataset_id,action,actor,detail) VALUES($1,$2,'snapshot-accepted',$3,$4)`, key.Owner, key.Dataset, actor, detail)
		return err
	}
	return s.PublishGuarded(ctx, key, generation, guard)
}
