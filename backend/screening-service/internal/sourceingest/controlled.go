// Package sourceingest stages controlled records. It has no HTTP publication
// endpoint, external connector, customer-selected URL or production seed data.
package sourceingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"strings"
)

const MaxBytes = 10 * 1024 * 1024
const MaxRecords = 50000
const MaxSourceBytes = 64 * 1024 * 1024

type Key struct {
	Owner   uuid.UUID
	Dataset string
}
type Store struct {
	DB            *pgxpool.Pool
	Scope         *uuid.UUID
	Guard         func(context.Context, pgx.Tx) error
	Finalize      func(context.Context, pgx.Tx, Publication) error
	OnStaging     func(context.Context, pgx.Tx, uuid.UUID) error
	AllowEmpty    bool
	AllowDisabled bool
	GlobalOnly    bool
}
type Publication struct {
	Generation uuid.UUID `json:"generation"`
	Unchanged  bool      `json:"unchanged"`
}

func (s Store) publisher(ctx context.Context) (string, error) {
	var actor string
	var allowed bool
	err := s.DB.QueryRow(ctx, `SELECT current_user, has_table_privilege(current_user,'screening.source_versions','INSERT') AND has_table_privilege(current_user,'screening.source_datasets','UPDATE')`).Scan(&actor, &allowed)
	if err != nil || !allowed {
		return "", sourcedata.ErrPublish
	}
	return actor, nil
}

// Register is an explicit operator operation using the authenticated DB role.
// It cannot adopt/change an existing list, grant or active publication.
func (s Store) Register(ctx context.Context, key Key, label string, tenants []uuid.UUID) error {
	if err := s.checkKey(key); err != nil {
		return err
	}
	if _, err := s.publisher(ctx); err != nil {
		return err
	}
	ref := sourcedata.DatasetRef{Scope: "global", ID: key.Dataset}
	if !ref.Valid() || strings.TrimSpace(label) == "" || len(label) > 200 || len(tenants) > 1000 {
		return sourcedata.ErrInput
	}
	for _, t := range tenants {
		if t == uuid.Nil || (key.Owner != uuid.Nil && t != key.Owner) {
			return sourcedata.ErrInput
		}
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	category := "controlled_fixture"
	if key.Owner != uuid.Nil {
		category = "tenant_watchlist"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_datasets(owner_scope,id,label,category) VALUES($1,$2,$3,$4)`, key.Owner, key.Dataset, label, category); err != nil {
		return sourcedata.ErrConflict
	}
	for _, t := range tenants {
		if _, err = tx.Exec(ctx, `INSERT INTO screening.source_grants(tenant_id,owner_scope,dataset_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, t, key.Owner, key.Dataset); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s Store) Stage(ctx context.Context, key Key, input io.Reader) (generation uuid.UUID, resultErr error) {
	if !sourcedata.NormalizerSupported() {
		return uuid.Nil, sourcedata.ErrNotReady
	}
	if err := s.checkKey(key); err != nil {
		return uuid.Nil, err
	}
	actor, err := s.publisher(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	generation = uuid.New()
	initial, err := s.begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	initialTag, err := initial.Exec(ctx, `INSERT INTO screening.source_versions(owner_scope,dataset_id,id,parent_generation,parent_revision,status,actor,normalizer)
 SELECT owner_scope,id,$3,active_generation,publication_revision,'staging',$4,$6 FROM screening.source_datasets WHERE owner_scope=$1 AND id=$2 AND (enabled OR $5)`, key.Owner, key.Dataset, generation, actor, s.AllowDisabled && s.Scope != nil, sourcedata.Normalizer)
	if err == nil && initialTag.RowsAffected() != 1 {
		err = sourcedata.ErrDataset
	}
	if err == nil && s.OnStaging != nil {
		err = s.OnStaging(ctx, initial, generation)
	}
	if err == nil {
		err = initial.Commit(ctx)
	} else {
		initial.Rollback(ctx)
	}
	if err != nil {
		return uuid.Nil, err
	}
	defer func() {
		if resultErr != nil {
			clean, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5_000_000_000)
			defer cancel()
			_, _ = s.DB.Exec(clean, `UPDATE screening.source_versions SET status='failed' WHERE owner_scope=$1 AND dataset_id=$2 AND id=$3 AND status='staging'`, key.Owner, key.Dataset, generation)
		}
	}()
	limited := &io.LimitedReader{R: input, N: MaxBytes + 1}
	var artifact bytes.Buffer
	d := json.NewDecoder(io.TeeReader(limited, &artifact))
	d.DisallowUnknownFields()
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return generation, sourcedata.ErrInput
	}
	seen := map[string]bool{}
	count := 0
	version := 0
	source := ""
	parser := "source-json-v2"
	controlledCompatible := true
	recordLimit := MaxRecords
	batch := make([]sourcedata.Record, 0, 200)
	for d.More() {
		token, e := d.Token()
		if e != nil {
			return generation, sourcedata.ErrInput
		}
		name, ok := token.(string)
		if !ok || seen[name] {
			return generation, sourcedata.ErrInput
		}
		seen[name] = true
		switch name {
		case "version":
			if d.Decode(&version) != nil || (version != 1 && version != 2) {
				return generation, sourcedata.ErrInput
			}
			if version == 2 {
				if seen["records"] {
					return generation, sourcedata.ErrInput
				}
				limited.N += MaxSourceBytes - MaxBytes
				recordLimit = 100000
			}
		case "source":
			if d.Decode(&source) != nil || !(sourcedata.DatasetRef{Scope: "global", ID: source}).Valid() {
				return generation, sourcedata.ErrInput
			}
		case "parser":
			if d.Decode(&parser) != nil || !(sourcedata.DatasetRef{Scope: "global", ID: parser}).Valid() {
				return generation, sourcedata.ErrInput
			}
		case "records":
			token, e := d.Token()
			if e != nil || token != json.Delim('[') {
				return generation, sourcedata.ErrInput
			}
			for d.More() {
				var r sourcedata.Record
				if d.Decode(&r) != nil || r.Validate() != nil {
					return generation, sourcedata.ErrInput
				}
				count++
				controlledCompatible = controlledCompatible && r.ValidateControlled() == nil
				if count > recordLimit {
					return generation, sourcedata.ErrInput
				}
				batch = append(batch, r)
				if len(batch) == cap(batch) {
					if e = s.writeBatch(ctx, key, generation, batch); e != nil {
						return generation, e
					}
					batch = batch[:0]
				}
			}
			if token, e = d.Token(); e != nil || token != json.Delim(']') {
				return generation, sourcedata.ErrInput
			}
		default:
			return generation, sourcedata.ErrInput
		}
	}
	// Empty snapshots are only explicit reviewed withdrawals/clears. Automatic
	// collectors cannot turn an unexpectedly empty source into mass removal.
	allowEmpty := s.AllowEmpty && ((s.Scope != nil && key.Owner != uuid.Nil) || (s.GlobalOnly && s.Guard != nil && key.Owner == uuid.Nil))
	if tok, err = d.Token(); err != nil || tok != json.Delim('}') || !seen["version"] || !seen["source"] || !seen["records"] || (count == 0 && !allowEmpty) {
		return generation, sourcedata.ErrInput
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) || limited.N <= 0 {
		return generation, sourcedata.ErrInput
	}
	if version == 1 {
		if source != "controlled-fixture" || seen["parser"] || !controlledCompatible {
			return generation, sourcedata.ErrInput
		}
		parser = "controlled-json-v1"
	} else {
		var registered bool
		if source == "tenant-upload" && key.Owner != uuid.Nil {
			registered = true
		} else {
			if err = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening.source_connections WHERE owner_scope=$1 AND dataset_id=$2 AND connector=$3)`, key.Owner, key.Dataset, source).Scan(&registered); err != nil {
				return generation, err
			}
		}
		if !registered {
			return generation, sourcedata.ErrInput
		}
	}
	if len(batch) > 0 {
		if err = s.writeBatch(ctx, key, generation, batch); err != nil {
			return generation, err
		}
	}
	// Hash canonical JSONB records in stable source identity order. Formatting and
	// input order alone do not create a semantic content generation.
	rows, err := s.DB.Query(ctx, `SELECT record::text FROM screening.source_records WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3 ORDER BY record_id`, key.Owner, key.Dataset, generation)
	if err != nil {
		return generation, err
	}
	hash := sha256.New()
	for rows.Next() {
		var r string
		if err = rows.Scan(&r); err != nil {
			rows.Close()
			return generation, err
		}
		hash.Write([]byte(r))
		hash.Write([]byte{0})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return generation, err
	}
	final, err := s.begin(ctx)
	if err != nil {
		return generation, err
	}
	defer final.Rollback(ctx)
	artifactHash := sha256.Sum256(artifact.Bytes())
	digest := hex.EncodeToString(artifactHash[:])
	if _, err = final.Exec(ctx, `INSERT INTO screening.source_artifacts(owner_scope,dataset_id,sha256,body,media_type) VALUES($1,$2,$3,$4,'application/json') ON CONFLICT DO NOTHING`, key.Owner, key.Dataset, digest, artifact.Bytes()); err != nil {
		return generation, err
	}
	if _, err = final.Exec(ctx, `INSERT INTO screening.source_version_artifacts(owner_scope,dataset_id,generation,sha256) VALUES($1,$2,$3,$4)`, key.Owner, key.Dataset, generation, digest); err != nil {
		return generation, err
	}
	tag, err := final.Exec(ctx, `UPDATE screening.source_versions SET status='staged',record_count=$4,content_hash=$5,source_key=$6,parser_version=$7 WHERE owner_scope=$1 AND dataset_id=$2 AND id=$3 AND status='staging' AND lease_until>now()`, key.Owner, key.Dataset, generation, count, hex.EncodeToString(hash.Sum(nil)), source, parser)
	if err != nil {
		return generation, err
	}
	if tag.RowsAffected() != 1 {
		return generation, sourcedata.ErrConflict
	}
	return generation, final.Commit(ctx)
}

func (s Store) writeBatch(ctx context.Context, key Key, generation uuid.UUID, records []sourcedata.Record) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	recordRows := make([][]any, 0, len(records))
	nameRows := make([][]any, 0, len(records)*2)
	identifierRows := [][]any{}
	for _, r := range records {
		raw, _ := json.Marshal(r)
		entity := uuid.NewSHA1(uuid.NameSpaceOID, []byte(key.Owner.String()+":"+key.Dataset+":"+r.ID))
		recordRows = append(recordRows, []any{key.Owner, key.Dataset, generation, r.ID, entity, r.Type, raw})
		seenIDs := map[string]bool{}
		for _, id := range r.Identifiers {
			normalized := sourcedata.NormalizeIdentifier(id.Value)
			unique := id.Type + "\x00" + normalized + "\x00" + id.Country + "\x00" + id.Issuer
			if seenIDs[unique] {
				continue
			}
			seenIDs[unique] = true
			identifierRows = append(identifierRows, []any{key.Owner, key.Dataset, generation, r.ID, id.Type, normalized, id.Country, id.Issuer, id.Value})
		}
		names := append([]string{r.Name}, r.Aliases...)
		seen := map[string]bool{}
		for _, name := range names {
			normal := sourcedata.Normalize(name)
			if seen[normal] {
				continue
			}
			seen[normal] = true
			nameRows = append(nameRows, []any{key.Owner, key.Dataset, generation, r.ID, normal, name})
		}
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"screening", "source_records"}, []string{"owner_scope", "dataset_id", "generation", "record_id", "entity_id", "entity_type", "record"}, pgx.CopyFromRows(recordRows)); err != nil {
		return copyError(err)
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"screening", "source_names"}, []string{"owner_scope", "dataset_id", "generation", "record_id", "normalized", "original"}, pgx.CopyFromRows(nameRows)); err != nil {
		return copyError(err)
	}
	if len(identifierRows) > 0 {
		if _, err = tx.CopyFrom(ctx, pgx.Identifier{"screening", "source_identifiers"}, []string{"owner_scope", "dataset_id", "generation", "record_id", "type", "normalized", "country", "issuer", "original"}, pgx.CopyFromRows(identifierRows)); err != nil {
			return copyError(err)
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE screening.source_versions SET lease_until=now()+interval '10 minutes' WHERE owner_scope=$1 AND dataset_id=$2 AND id=$3 AND status='staging' AND lease_until>now()`, key.Owner, key.Dataset, generation)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return sourcedata.ErrConflict
	}
	return tx.Commit(ctx)
}

func (s Store) Publish(ctx context.Context, key Key, generation uuid.UUID) (Publication, error) {
	return s.PublishGuarded(ctx, key, generation, nil)
}

func copyError(err error) error {
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && (strings.HasPrefix(pgerr.Code, "22") || strings.HasPrefix(pgerr.Code, "23")) {
		return sourcedata.ErrInput
	}
	return err // cancellation, timeout and database failures remain retryable
}

// PublishGuarded checks a collector's persisted lease/fence inside the same
// short publication transaction. No fetch/parse/provider IO belongs in guard.
func (s Store) PublishGuarded(ctx context.Context, key Key, generation uuid.UUID, guard func(context.Context, pgx.Tx) error) (Publication, error) {
	if err := s.checkKey(key); err != nil {
		return Publication{}, err
	}
	actor, err := s.publisher(ctx)
	if err != nil {
		return Publication{}, err
	}
	// Staging has committed. Refresh large projections before publication locks;
	// fresh small-table estimates otherwise cause a measured ~330ms name scan.
	var large bool
	if err = s.DB.QueryRow(ctx, `SELECT record_count>=5000 FROM screening.source_versions WHERE owner_scope=$1 AND dataset_id=$2 AND id=$3`, key.Owner, key.Dataset, generation).Scan(&large); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Publication{}, sourcedata.ErrConflict
		}
		return Publication{}, err
	}
	if large {
		var refreshed bool
		if err = s.DB.QueryRow(ctx, `SELECT screening.refresh_source_statistics_v1()`).Scan(&refreshed); err != nil {
			return Publication{}, err
		}
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return Publication{}, err
	}
	defer tx.Rollback(ctx)
	if guard != nil {
		if err = guard(ctx, tx); err != nil {
			return Publication{}, err
		}
	} else {
		// Manual source publication/rollback is not a fresh external check.
		// Pause automatic collection and invalidate its fence before the dataset
		// lock; an older in-flight collector cannot undo the reviewed operation.
		if _, err = tx.Exec(ctx, `UPDATE screening.source_connections SET enabled=false,lease_token=NULL,lease_until=NULL,fence=fence+1,last_error='quarantined_manual_publication' WHERE owner_scope=$1 AND dataset_id=$2`, key.Owner, key.Dataset); err != nil {
			return Publication{}, err
		}
	}
	var current *uuid.UUID
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT active_generation,publication_revision FROM screening.source_datasets WHERE owner_scope=$1 AND id=$2 AND (enabled OR $3) FOR NO KEY UPDATE`, key.Owner, key.Dataset, s.AllowDisabled && s.Scope != nil).Scan(&current, &revision); err != nil {
		return Publication{}, sourcedata.ErrDataset
	}
	var parent *uuid.UUID
	var parentRevision int64
	var status, hash, normalizer string
	finish := func(result Publication) (Publication, error) {
		if s.Finalize != nil {
			if e := s.Finalize(ctx, tx, result); e != nil {
				return Publication{}, e
			}
		}
		return result, tx.Commit(ctx)
	}
	if err = tx.QueryRow(ctx, `SELECT parent_generation,parent_revision,status,content_hash,normalizer FROM screening.source_versions WHERE owner_scope=$1 AND dataset_id=$2 AND id=$3 FOR NO KEY UPDATE`, key.Owner, key.Dataset, generation).Scan(&parent, &parentRevision, &status, &hash, &normalizer); err != nil {
		return Publication{}, sourcedata.ErrConflict
	}
	if current != nil && *current == generation && status == "published" {
		return finish(Publication{Generation: generation, Unchanged: true})
	}
	if status != "staged" || revision != parentRevision || (current == nil) != (parent == nil) || (current != nil && *current != *parent) {
		return Publication{}, sourcedata.ErrConflict
	}
	if current != nil {
		var activeHash, activeNormalizer string
		if err = tx.QueryRow(ctx, `SELECT content_hash,normalizer FROM screening.source_versions WHERE owner_scope=$1 AND dataset_id=$2 AND id=$3`, key.Owner, key.Dataset, *current).Scan(&activeHash, &activeNormalizer); err != nil {
			return Publication{}, err
		}
		if activeHash == hash && activeNormalizer == normalizer {
			return finish(Publication{Generation: *current, Unchanged: true})
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE screening.source_versions SET status='published',published_at=now() WHERE owner_scope=$1 AND dataset_id=$2 AND id=$3`, key.Owner, key.Dataset, generation); err != nil {
		return Publication{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE screening.source_datasets SET active_generation=$3,publication_revision=publication_revision+1 WHERE owner_scope=$1 AND id=$2`, key.Owner, key.Dataset, generation); err != nil {
		return Publication{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_publications(owner_scope,dataset_id,generation,previous_generation,actor) VALUES($1,$2,$3,$4,$5)`, key.Owner, key.Dataset, generation, current, actor); err != nil {
		return Publication{}, err
	}
	return finish(Publication{Generation: generation})
}
