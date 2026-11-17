package privatelists

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	DB     *pgxpool.Pool
	Policy Policy
}
type CreateInput struct {
	OperationID     string `json:"operation_id"`
	Name            string `json:"name"`
	Purpose         string `json:"purpose"`
	RightsReference string `json:"rights_reference"`
}
type ImportInput struct {
	OperationID       string `json:"operation_id"`
	Action            string `json:"action"`
	CSV               string `json:"csv,omitempty"`
	RestoreGeneration string `json:"restore_generation,omitempty"`
}
type Import struct {
	ID                  uuid.UUID  `json:"id"`
	Dataset             string     `json:"dataset_id"`
	Status              string     `json:"status"`
	Action              string     `json:"action"`
	Generation          *uuid.UUID `json:"generation"`
	PublishedGeneration *uuid.UUID `json:"published_generation"`
	CreatedBy           uuid.UUID  `json:"created_by"`
	Count               int        `json:"record_count"`
	Hash                string     `json:"content_hash"`
}

func (s Service) begin(ctx context.Context, p identity.Principal, action string) (pgx.Tx, error) {
	if s.DB == nil {
		return nil, ErrPolicy
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.Policy.check(ctx, tx, p, action, true); err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}
func (s Service) scoped(p identity.Principal, action string) sourceingest.Store {
	return sourceingest.Store{DB: s.DB, Scope: &p.TenantID, AllowDisabled: true, Guard: func(ctx context.Context, tx pgx.Tx) error { return s.Policy.check(ctx, tx, p, action, true) }}
}

func (s Service) Create(ctx context.Context, p identity.Principal, in CreateInput) (string, error) {
	op, err := uuid.Parse(in.OperationID)
	in.Name = strings.TrimSpace(in.Name)
	in.Purpose = strings.TrimSpace(in.Purpose)
	in.RightsReference = strings.TrimSpace(in.RightsReference)
	if err != nil || op == uuid.Nil || in.Name == "" || len(in.Name) > 200 || in.Purpose == "" || len(in.Purpose) > 1000 || in.RightsReference == "" || len(in.RightsReference) > 500 {
		return "", ErrInput
	}
	tx, err := s.begin(ctx, p, "import")
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	dataset := "private-" + strings.ReplaceAll(uuid.NewSHA1(uuid.NameSpaceOID, []byte(p.TenantID.String()+":"+op.String())).String(), "-", "")
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, p.TenantID.String()+":"+op.String()); err != nil {
		return "", err
	}
	var existingName, purpose, rights string
	err = tx.QueryRow(ctx, `SELECT name,purpose,rights_reference FROM screening.private_lists WHERE tenant_id=$1 AND dataset_id=$2`, p.TenantID, dataset).Scan(&existingName, &purpose, &rights)
	if err == nil {
		if existingName != in.Name || purpose != in.Purpose || rights != in.RightsReference {
			return "", ErrConflict
		}
		return dataset, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_datasets(owner_scope,id,label,category) VALUES($1,$2,$3,'tenant_watchlist')`, p.TenantID, dataset, in.Name); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_grants(tenant_id,owner_scope,dataset_id) VALUES($1,$1,$2)`, p.TenantID, dataset); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.private_lists(tenant_id,dataset_id,name,purpose,rights_reference,created_by) VALUES($1,$2,$3,$4,$5,$6)`, p.TenantID, dataset, in.Name, in.Purpose, in.RightsReference, p.Subject); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.private_list_audit(tenant_id,dataset_id,actor,action) VALUES($1,$2,$3,'created')`, p.TenantID, dataset, p.Subject); err != nil {
		return "", err
	}
	return dataset, tx.Commit(ctx)
}

func (s Service) Submit(ctx context.Context, p identity.Principal, dataset string, in ImportInput) (Import, error) {
	if s.DB == nil {
		return Import{}, ErrPolicy
	}
	if err := s.Policy.check(ctx, s.DB, p, "import", false); err != nil {
		return Import{}, err
	}
	op, err := uuid.Parse(in.OperationID)
	if err != nil || op == uuid.Nil || len(dataset) > 80 {
		return Import{}, ErrInput
	}
	if in.Action == "" {
		in.Action = "snapshot"
	}
	if in.Action != "snapshot" && in.Action != "clear" && in.Action != "archive" && in.Action != "restore" {
		return Import{}, ErrInput
	}
	var records any
	var retained uuid.UUID
	if in.Action == "snapshot" {
		parsed, e := ParseCSV(in.CSV)
		if e != nil {
			return Import{}, e
		}
		records = parsed
	} else {
		if in.CSV != "" {
			return Import{}, ErrInput
		}
		records = []any{}
	}
	if in.Action == "restore" {
		retained, err = uuid.Parse(in.RestoreGeneration)
		if err != nil || retained == uuid.Nil {
			return Import{}, ErrInput
		}
	} else if in.RestoreGeneration != "" {
		return Import{}, ErrInput
	}
	bytesIn, _ := json.Marshal([]any{dataset, in, p.Subject})
	requestHash := sha256.Sum256(bytesIn)
	requestDigest := hex.EncodeToString(requestHash[:])
	artifact := []byte(in.CSV)
	if in.Action != "snapshot" {
		artifact = []byte("record_id,name,entity_type,source_reference\n")
	}
	rawHash := sha256.Sum256(artifact)
	digest := hex.EncodeToString(rawHash[:])
	tx, err := s.begin(ctx, p, "import")
	if err != nil {
		return Import{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, p.TenantID.String()+":"+op.String()); err != nil {
		return Import{}, err
	}
	var oldID uuid.UUID
	var oldHash string
	err = tx.QueryRow(ctx, `SELECT id,request_hash FROM screening.private_imports WHERE tenant_id=$1 AND operation_id=$2`, p.TenantID, op).Scan(&oldID, &oldHash)
	if err == nil {
		if oldHash != requestDigest {
			return Import{}, ErrConflict
		}
		tx.Rollback(ctx)
		return s.GetImport(ctx, p, dataset, oldID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Import{}, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening.private_lists WHERE tenant_id=$1 AND dataset_id=$2)`, p.TenantID, dataset).Scan(&exists); err != nil {
		return Import{}, err
	}
	if !exists {
		return Import{}, ErrNotFound
	}
	id := uuid.New()
	if in.Action != "restore" {
		if _, err = tx.Exec(ctx, `INSERT INTO screening.source_artifacts(owner_scope,dataset_id,sha256,body,media_type) VALUES($1,$2,$3,$4,'text/csv') ON CONFLICT DO NOTHING`, p.TenantID, dataset, digest, artifact); err != nil {
			return Import{}, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.private_imports(id,tenant_id,dataset_id,operation_id,request_hash,artifact_sha256,action,status,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,'staging',$8)`, id, p.TenantID, dataset, op, requestDigest, digest, in.Action, p.Subject); err != nil {
		return Import{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.private_list_audit(tenant_id,dataset_id,import_id,actor,action) VALUES($1,$2,$3,$4,'import-started')`, p.TenantID, dataset, id, p.Subject); err != nil {
		return Import{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Import{}, err
	}
	ingest := s.scoped(p, "import")
	ingest.AllowEmpty = in.Action == "clear" || in.Action == "archive"
	ingest.OnStaging = func(ctx context.Context, tx pgx.Tx, generation uuid.UUID) error {
		tag, e := tx.Exec(ctx, `UPDATE screening.private_imports SET generation=$4 WHERE tenant_id=$1 AND dataset_id=$2 AND id=$3 AND status='staging'`, p.TenantID, dataset, id, generation)
		if e == nil && tag.RowsAffected() != 1 {
			return ErrConflict
		}
		return e
	}
	key := sourceingest.Key{Owner: p.TenantID, Dataset: dataset}
	var generation uuid.UUID
	if in.Action == "restore" {
		generation, err = ingest.StageRollback(ctx, key, retained)
	} else {
		envelope, _ := json.Marshal(struct {
			Version int    `json:"version"`
			Source  string `json:"source"`
			Parser  string `json:"parser"`
			Records any    `json:"records"`
		}{2, "tenant-upload", "tenant-csv-v1", records})
		generation, err = ingest.Stage(ctx, key, bytes.NewReader(envelope))
	}
	if err != nil {
		// No active data changed. A failed request remains an explicit receipt. Raw
		// CSV is retained so a current authorized creator can inspect/re-upload it.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5_000_000_000)
		defer cancel()
		_, _ = s.DB.Exec(cleanup, `UPDATE screening.private_imports SET status='failed',diagnostic='staging_failed' WHERE tenant_id=$1 AND id=$2 AND status='staging'`, p.TenantID, id)
		return Import{}, err
	}
	finish, err := s.begin(ctx, p, "import")
	if err != nil {
		return Import{}, err
	}
	defer finish.Rollback(ctx)
	if in.Action == "restore" {
		if err = finish.QueryRow(ctx, `SELECT sha256 FROM screening.source_version_artifacts WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3`, p.TenantID, dataset, generation).Scan(&digest); err != nil {
			return Import{}, err
		}
	} else {
		var normalizedHash string
		if err = finish.QueryRow(ctx, `SELECT sha256 FROM screening.source_version_artifacts WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3`, p.TenantID, dataset, generation).Scan(&normalizedHash); err != nil {
			return Import{}, err
		}
		if _, err = finish.Exec(ctx, `UPDATE screening.source_version_artifacts SET sha256=$4 WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3`, p.TenantID, dataset, generation, digest); err != nil {
			return Import{}, err
		}
		if _, err = finish.Exec(ctx, `DELETE FROM screening.source_artifacts a WHERE owner_scope=$1 AND dataset_id=$2 AND sha256=$3 AND NOT EXISTS(SELECT 1 FROM screening.source_version_artifacts v WHERE(v.owner_scope,v.dataset_id,v.sha256)=(a.owner_scope,a.dataset_id,a.sha256))`, p.TenantID, dataset, normalizedHash); err != nil {
			return Import{}, err
		}
	}
	_, err = finish.Exec(ctx, `UPDATE screening.private_imports i SET status='pending_review',generation=$4,artifact_sha256=$5,record_count=v.record_count,content_hash=v.content_hash FROM screening.source_versions v WHERE i.tenant_id=$1 AND i.dataset_id=$2 AND i.id=$3 AND i.status='staging' AND(v.owner_scope,v.dataset_id,v.id)=($1,$2,$4) AND v.status='staged'`, p.TenantID, dataset, id, generation, digest)
	if err != nil {
		return Import{}, err
	}
	if _, err = finish.Exec(ctx, `INSERT INTO screening.private_list_audit(tenant_id,dataset_id,import_id,actor,action) VALUES($1,$2,$3,$4,'submitted-for-review')`, p.TenantID, dataset, id, p.Subject); err != nil {
		return Import{}, err
	}
	if err = finish.Commit(ctx); err != nil {
		return Import{}, err
	}
	return s.GetImport(ctx, p, dataset, id)
}

func (s Service) GetImport(ctx context.Context, p identity.Principal, dataset string, id uuid.UUID) (Import, error) {
	if err := s.Policy.check(ctx, s.DB, p, "read", false); err != nil {
		return Import{}, err
	}
	out := Import{}
	err := s.DB.QueryRow(ctx, `SELECT id,dataset_id,status,action,generation,published_generation,created_by,record_count,coalesce(content_hash,'') FROM screening.private_imports WHERE tenant_id=$1 AND dataset_id=$2 AND id=$3`, p.TenantID, dataset, id).Scan(&out.ID, &out.Dataset, &out.Status, &out.Action, &out.Generation, &out.PublishedGeneration, &out.CreatedBy, &out.Count, &out.Hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}

func (s Service) Approve(ctx context.Context, p identity.Principal, dataset string, id uuid.UUID) (Import, error) {
	item, err := s.GetImport(ctx, p, dataset, id)
	if err != nil {
		return Import{}, err
	}
	if item.CreatedBy.String() == p.Subject {
		return Import{}, identity.Forbidden()
	}
	if item.Status == "published" {
		return item, s.Policy.check(ctx, s.DB, p, "approve", false)
	}
	if item.Status != "pending_review" || item.Generation == nil {
		return Import{}, ErrConflict
	}
	ingest := s.scoped(p, "approve")
	ingest.Finalize = func(ctx context.Context, tx pgx.Tx, result sourceingest.Publication) error {
		var creator, generation uuid.UUID
		var status, hash, action string
		if e := tx.QueryRow(ctx, `SELECT created_by,status,content_hash,action,generation FROM screening.private_imports WHERE tenant_id=$1 AND dataset_id=$2 AND id=$3 FOR UPDATE`, p.TenantID, dataset, id).Scan(&creator, &status, &hash, &action, &generation); e != nil {
			return e
		}
		if creator.String() == p.Subject {
			return identity.Forbidden()
		}
		if status != "pending_review" || hash != item.Hash || generation != *item.Generation {
			return ErrConflict
		}
		if _, e := tx.Exec(ctx, `UPDATE screening.private_imports SET status='published',published_generation=$4,reviewed_by=$5,reviewed_at=now() WHERE tenant_id=$1 AND dataset_id=$2 AND id=$3`, p.TenantID, dataset, id, result.Generation, p.Subject); e != nil {
			return e
		}
		archived := action == "archive"
		if _, e := tx.Exec(ctx, `UPDATE screening.private_lists SET archived=$3 WHERE tenant_id=$1 AND dataset_id=$2`, p.TenantID, dataset, archived); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `UPDATE screening.source_datasets SET enabled=NOT $3 WHERE owner_scope=$1 AND id=$2`, p.TenantID, dataset, archived); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO screening.private_list_audit(tenant_id,dataset_id,import_id,actor,action,detail) VALUES($1,$2,$3,$4,'approved',jsonb_build_object('generation',$5::text,'action',$6::text))`, p.TenantID, dataset, id, p.Subject, result.Generation.String(), action)
		return e
	}
	// The content/parent fence and import review commit with pointer/event, source
	// evidence and archive state. Failed approval cannot publish half a command.
	_, err = ingest.PublishGuarded(ctx, sourceingest.Key{Owner: p.TenantID, Dataset: dataset}, *item.Generation, func(ctx context.Context, tx pgx.Tx) error {
		var valid bool
		e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening.source_versions WHERE owner_scope=$1 AND dataset_id=$2 AND id=$3 AND content_hash=$4 AND status='staged')`, p.TenantID, dataset, *item.Generation, item.Hash).Scan(&valid)
		if e != nil {
			return e
		}
		if !valid {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return Import{}, err
	}
	return s.GetImport(ctx, p, dataset, id)
}
