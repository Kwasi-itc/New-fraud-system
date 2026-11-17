package sourcecollector

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconfig"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconnectors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Collector struct {
	DB      *pgxpool.Pool
	Fetcher sourceconnectors.Fetcher
	Dir     string
}
type Lease struct {
	Key                            sourceingest.Key
	Token, Run                     uuid.UUID
	Fence                          int64
	Connector, ETag, LastModified  string
	Config                         json.RawMessage
	HasPublication, UseConditional bool
	Generation                     *uuid.UUID
}
type configuration struct {
	Coverage       string `json:"coverage"`
	Reference      string `json:"reference"`
	Parser         string `json:"parser"`
	RightsNote     string `json:"rights_note"`
	URLSecretEnv   string `json:"url_secret_env,omitempty"`
	MaxDropPercent *int   `json:"max_drop_percent,omitempty"`
}

func (c Collector) Claim(ctx context.Context) (*Lease, error) {
	tx, err := c.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	l := &Lease{Token: uuid.New(), Run: uuid.New()}
	err = tx.QueryRow(ctx, `SELECT c.owner_scope,c.dataset_id,c.connector,c.fence+1,c.etag,c.last_modified,c.config,d.active_generation IS NOT NULL,coalesce(c.verified_generation=d.active_generation AND active.normalizer=$1,false),d.active_generation FROM screening.source_connections c JOIN screening.source_datasets d ON (d.owner_scope,d.id)=(c.owner_scope,c.dataset_id) LEFT JOIN screening.source_versions active ON(active.owner_scope,active.dataset_id,active.id)=(d.owner_scope,d.id,d.active_generation) WHERE c.enabled AND d.enabled AND c.rights_state='approved' AND c.rights_review_reference<>'' AND c.next_check_at<=now() AND (c.lease_until IS NULL OR c.lease_until<=now()) ORDER BY c.next_check_at,c.dataset_id FOR NO KEY UPDATE OF c SKIP LOCKED LIMIT 1`, sourcedata.Normalizer).Scan(&l.Key.Owner, &l.Key.Dataset, &l.Connector, &l.Fence, &l.ETag, &l.LastModified, &l.Config, &l.HasPublication, &l.UseConditional, &l.Generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE screening.source_connections SET lease_token=$3,lease_until=now()+interval '10 minutes',fence=$4,last_attempt_at=now() WHERE owner_scope=$1 AND dataset_id=$2`, l.Key.Owner, l.Key.Dataset, l.Token, l.Fence); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE screening.source_import_runs r SET status=CASE WHEN EXISTS(SELECT 1 FROM screening.source_publications p WHERE(p.owner_scope,p.dataset_id,p.generation)=(r.owner_scope,r.dataset_id,r.generation)) THEN 'published' ELSE 'failed' END,diagnostic='interrupted_previous_lease',completed_at=now() WHERE r.owner_scope=$1 AND r.dataset_id=$2 AND r.fence<$3 AND r.status IN ('fetching','parsing','staged')`, l.Key.Owner, l.Key.Dataset, l.Fence); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_import_runs(id,owner_scope,dataset_id,fence,status) VALUES($1,$2,$3,$4,'fetching')`, l.Run, l.Key.Owner, l.Key.Dataset, l.Fence); err != nil {
		return nil, err
	}
	return l, tx.Commit(ctx)
}

func (c Collector) guard(l Lease) func(context.Context, pgx.Tx) error {
	return func(ctx context.Context, tx pgx.Tx) error {
		var allowed bool
		err := tx.QueryRow(ctx, `SELECT enabled AND rights_state='approved' AND lease_until>clock_timestamp() FROM screening.source_connections WHERE owner_scope=$1 AND dataset_id=$2 AND lease_token=$3 AND fence=$4 FOR NO KEY UPDATE`, l.Key.Owner, l.Key.Dataset, l.Token, l.Fence).Scan(&allowed)
		if err != nil || !allowed {
			return sourcedata.ErrConflict
		}
		return nil
	}
}

func (c Collector) PublishStaged(ctx context.Context, l Lease, generation uuid.UUID) (sourceingest.Publication, error) {
	return (sourceingest.Store{DB: c.DB, GlobalOnly: true}).PublishGuarded(ctx, l.Key, generation, func(ctx context.Context, tx pgx.Tx) error {
		if err := c.guard(l)(ctx, tx); err != nil {
			return err
		}
		var valid bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening.source_import_runs WHERE id=$1 AND owner_scope=$2 AND dataset_id=$3 AND fence=$4 AND generation=$5 AND status='staged')`, l.Run, l.Key.Owner, l.Key.Dataset, l.Fence, generation).Scan(&valid); err != nil || !valid {
			return sourcedata.ErrConflict
		}
		return nil
	})
}

func (c Collector) finish(ctx context.Context, l Lease, status, diagnostic string, download sourceconnectors.Download, generation *uuid.UUID, count int) error {
	tx, err := c.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = c.guard(l)(ctx, tx); err != nil {
		return err
	}
	success := status == "published" || status == "unchanged"
	quarantine := status == "quarantined"
	var previousFailures int
	if err = tx.QueryRow(ctx, `SELECT failures FROM screening.source_connections WHERE owner_scope=$1 AND dataset_id=$2`, l.Key.Owner, l.Key.Dataset).Scan(&previousFailures); err != nil {
		return err
	}
	if !success && !quarantine && previousFailures >= 4 {
		quarantine = true
		diagnostic = "quarantined_retry_exhausted"
	}
	// Bounded persisted jitter/backoff. Parser/schema/drop failures require manual
	// action; transient failures retry and never update last successful freshness.
	var randomBytes [2]byte
	if _, err = rand.Read(randomBytes[:]); err != nil {
		return err
	}
	jitter := int(binary.BigEndian.Uint16(randomBytes[:]) % 11)
	retrySeconds := int(download.RetryAfter.Seconds())
	_, err = tx.Exec(ctx, `UPDATE screening.source_connections SET lease_token=NULL,lease_until=NULL,enabled=CASE WHEN $6 THEN false ELSE enabled END,
 last_error=$5,last_success_at=CASE WHEN $7 THEN now() ELSE last_success_at END,verified_generation=CASE WHEN $7 THEN $12 ELSE verified_generation END,
 failures=CASE WHEN $7 THEN 0 ELSE least(failures+1,10) END,
 etag=CASE WHEN $7 AND $8<>'' THEN $8 ELSE etag END,last_modified=CASE WHEN $7 AND $9<>'' THEN $9 ELSE last_modified END,
 next_check_at=CASE WHEN $7 OR $6 THEN now()+poll_interval ELSE now()+make_interval(secs=>greatest($10,least(3600,30*power(2,least(failures,6))::int))+$11) END
 WHERE owner_scope=$1 AND dataset_id=$2 AND lease_token=$3 AND fence=$4`, l.Key.Owner, l.Key.Dataset, l.Token, l.Fence, diagnostic, quarantine, success, download.ETag, download.LastModified, retrySeconds, jitter, generation)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE screening.source_import_runs SET status=$2,diagnostic=$3,generation=coalesce($4,generation),artifact_sha256=nullif($5,''),record_count=$6,completed_at=now() WHERE id=$1 AND fence=$7`, l.Run, status, diagnostic, generation, download.SHA256, count, l.Fence)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RunOne records source failures and continues independent sources. A nil error
// means the attempt receipt was durably recorded, not that ingestion succeeded.
func (c Collector) RunOne(ctx context.Context) (bool, error) {
	l, err := c.Claim(ctx)
	if err != nil || l == nil {
		return false, err
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	spec, err := sourceconnectors.Lookup(l.Connector)
	config := configuration{}
	decoder := json.NewDecoder(strings.NewReader(string(l.Config)))
	decoder.DisallowUnknownFields()
	if err == nil {
		err = decoder.Decode(&config)
	}
	if err == nil && spec.URL == "" {
		if !regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,79}$`).MatchString(config.URLSecretEnv) {
			err = sourceconnectors.ErrDownload
		} else {
			spec.URL, err = sourceconfig.ReadSecret(config.URLSecretEnv)
			if err != nil || spec.URL == "" {
				err = sourceconnectors.ErrDownload
			}
		}
	}
	drop := 20
	if config.MaxDropPercent != nil {
		drop = *config.MaxDropPercent
	}
	if drop < 0 || drop > 100 {
		err = sourcedata.ErrInput
	}
	download := sourceconnectors.Download{}
	finish := func(status, code string, generation *uuid.UUID, count int) error {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		return c.finish(cleanup, *l, status, code, download, generation, count)
	}
	if err != nil {
		return true, finish("quarantined", "quarantined_source_configuration", nil, 0)
	}
	etag, lastModified := l.ETag, l.LastModified
	if !l.UseConditional {
		etag, lastModified = "", ""
	}
	download, err = c.Fetcher.Fetch(bounded, spec, c.Dir, etag, lastModified)
	if err != nil {
		return true, finish("failed", "source_download_failed", nil, 0)
	}
	if download.NotModified {
		if !l.HasPublication || !l.UseConditional {
			return true, finish("quarantined", "quarantined_unexpected_not_modified", nil, 0)
		}
		return true, finish("unchanged", "", l.Generation, 0)
	}
	defer os.Remove(download.Path)
	preserved, e := (Pipeline{DB: c.DB}).PreserveArtifact(bounded, l.Key, download.Path)
	if e != nil {
		return true, finish("failed", "source_artifact_storage_failed", nil, 0)
	}
	if preserved != download.SHA256 {
		return true, finish("quarantined", "quarantined_artifact_changed", nil, 0)
	}
	tag, err := c.DB.Exec(bounded, `UPDATE screening.source_import_runs r SET status='parsing',artifact_sha256=$2 FROM screening.source_connections c WHERE r.id=$1 AND r.status='fetching' AND (c.owner_scope,c.dataset_id,c.fence,c.lease_token)=(r.owner_scope,r.dataset_id,r.fence,$3::uuid) AND c.enabled AND c.lease_until>now()`, l.Run, download.SHA256, l.Token)
	if err != nil {
		return true, err
	}
	if tag.RowsAffected() != 1 {
		return true, sourcedata.ErrConflict
	}
	generation, parsed, err := (Pipeline{DB: c.DB}).StageSnapshot(bounded, l.Key, spec, download.Path)
	if err != nil {
		if errors.Is(err, sourceconnectors.ErrSchema) || errors.Is(err, sourceconnectors.ErrLimit) || errors.Is(err, sourcedata.ErrInput) {
			return true, finish("quarantined", "quarantined_source_validation", nil, parsed.Total)
		}
		return true, finish("failed", "source_storage_or_timeout", nil, parsed.Total)
	}
	tag, err = c.DB.Exec(bounded, `UPDATE screening.source_import_runs r SET status='staged',generation=$2,record_count=$3,publisher_date=$4 FROM screening.source_connections c WHERE r.id=$1 AND r.status='parsing' AND (c.owner_scope,c.dataset_id,c.fence,c.lease_token)=(r.owner_scope,r.dataset_id,r.fence,$5::uuid) AND c.enabled AND c.lease_until>now()`, l.Run, generation, parsed.Total, parsed.PublisherDate, l.Token)
	if err != nil {
		return true, err
	}
	if tag.RowsAffected() != 1 {
		return true, sourcedata.ErrConflict
	}
	var oldCount int
	if err = c.DB.QueryRow(bounded, `SELECT coalesce(parent.record_count,0) FROM screening.source_versions staged LEFT JOIN screening.source_versions parent ON (parent.owner_scope,parent.dataset_id,parent.id)=(staged.owner_scope,staged.dataset_id,staged.parent_generation) WHERE staged.owner_scope=$1 AND staged.dataset_id=$2 AND staged.id=$3`, l.Key.Owner, l.Key.Dataset, generation).Scan(&oldCount); err != nil {
		return true, err
	}
	if oldCount > 0 && parsed.Total*100 < oldCount*(100-drop) {
		return true, finish("quarantined", "quarantined_large_drop", &generation, parsed.Total)
	}
	publication, err := c.PublishStaged(bounded, *l, generation)
	if err != nil {
		return true, finish("failed", "source_publication_failed", &generation, parsed.Total)
	}
	status := "published"
	if publication.Unchanged {
		status = "unchanged"
	}
	return true, finish(status, "", &publication.Generation, parsed.Total)
}

func (c Collector) Recover(ctx context.Context, limit int) (int64, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	tag, err := c.DB.Exec(ctx, `WITH abandoned AS(SELECT r.id FROM screening.source_import_runs r JOIN screening.source_connections c ON (c.owner_scope,c.dataset_id)=(r.owner_scope,r.dataset_id) WHERE r.status IN ('fetching','parsing','staged') AND (r.fence<c.fence OR (c.lease_until IS NOT NULL AND c.lease_until<=now())) ORDER BY r.started_at,r.id FOR UPDATE OF r SKIP LOCKED LIMIT $1)
 UPDATE screening.source_import_runs r SET status=CASE WHEN EXISTS(SELECT 1 FROM screening.source_publications p WHERE(p.owner_scope,p.dataset_id,p.generation)=(r.owner_scope,r.dataset_id,r.generation)) THEN 'published' ELSE 'failed' END,diagnostic='interrupted_attempt_recovered',completed_at=now() FROM abandoned a WHERE r.id=a.id`, limit)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
