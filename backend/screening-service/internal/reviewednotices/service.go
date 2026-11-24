// Package reviewednotices implements controlled global notice curation, not a
// scraper. It requires global database publication authority PLUS a verified,
// current human platform identity. A tenant administrator alone grants no global
// source authority; customer APIs never expose this service.
package reviewednotices

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/privatelists"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconnectors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInput = errors.New("invalid_reviewed_notice")
var ErrAuthority = errors.New("global_notice_operator_authority_required")
var ErrConflict = errors.New("reviewed_notice_conflict")
var ErrNotFound = errors.New("reviewed_notice_not_found")

type Service struct{ DB *pgxpool.Pool }
type Input struct {
	OperationID     string `json:"operation_id"`
	Source          string `json:"source"`
	Dataset         string `json:"dataset_id"`
	Action          string `json:"action"`
	NoticeID        string `json:"notice_id"`
	Reference       string `json:"reference"`
	PublicationDate string `json:"publication_date"`
	RightsReference string `json:"rights_review_reference"`
	CSV             string `json:"csv"`
}
type Receipt struct {
	ID                  uuid.UUID  `json:"id"`
	Dataset             string     `json:"dataset_id"`
	Status              string     `json:"status"`
	CreatedBy           uuid.UUID  `json:"created_by"`
	Generation          *uuid.UUID `json:"generation"`
	PublishedGeneration *uuid.UUID `json:"published_generation"`
	Hash                string     `json:"content_hash"`
	Count               int        `json:"record_count"`
}

func (s Service) authorize(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, p identity.Principal, lock bool) error {
	// Machine/global authority comes from the separately provisioned operator DB
	// role. These privileges are not granted to tenant-private writers/collectors.
	var operator bool
	e := q.QueryRow(ctx, `SELECT has_table_privilege(current_user,'screening.reviewed_notice_imports','INSERT') AND has_table_privilege(current_user,'screening.reviewed_notice_imports','UPDATE') AND has_table_privilege(current_user,'screening.source_connections','UPDATE')`).Scan(&operator)
	if e != nil || !operator {
		return ErrAuthority
	}
	return identity.CheckCapability(ctx, q, p, "source-notices:read", lock)
}
func (s Service) begin(ctx context.Context, p identity.Principal) (pgx.Tx, error) {
	if s.DB == nil {
		return nil, ErrAuthority
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	if e = s.authorize(ctx, tx, p, true); e != nil {
		tx.Rollback(ctx)
		return nil, e
	}
	return tx, nil
}
func (s Service) store(p identity.Principal) sourceingest.Store {
	return sourceingest.Store{DB: s.DB, GlobalOnly: true, Guard: func(ctx context.Context, tx pgx.Tx) error { return s.authorize(ctx, tx, p, true) }}
}
func digest(raw []byte) string { hash := sha256.Sum256(raw); return hex.EncodeToString(hash[:]) }

func (s Service) Submit(ctx context.Context, p identity.Principal, in Input, attachment []byte) (Receipt, error) {
	if s.DB == nil {
		return Receipt{}, ErrAuthority
	}
	if e := s.authorize(ctx, s.DB, p, false); e != nil {
		return Receipt{}, e
	}
	op, e := uuid.Parse(in.OperationID)
	spec, se := sourceconnectors.NoticeSource(in.Source)
	date, de := time.Parse("2006-01-02", in.PublicationDate)
	if e != nil || op == uuid.Nil || se != nil || !spec.Intake || de != nil || date.After(time.Now().UTC()) || date.Year() < 1900 || !spec.ValidReference(in.Reference) || !(sourcedata.DatasetRef{Scope: "global", ID: in.Dataset}).Valid() || strings.TrimSpace(in.NoticeID) == "" || len(in.NoticeID) > 200 || strings.TrimSpace(in.RightsReference) == "" || len(in.RightsReference) > 500 || len(attachment) < 1 || len(attachment) > 10*1024*1024 {
		return Receipt{}, ErrInput
	}
	media := "application/pdf"
	if !bytes.HasPrefix(attachment, []byte("%PDF-")) {
		media = "text/plain"
		if !utf8.Valid(attachment) || bytes.Contains(attachment, []byte{0}) || bytes.Contains(bytes.ToLower(attachment), []byte("<html")) {
			return Receipt{}, ErrInput
		}
	}
	if in.Action == "" {
		in.Action = "snapshot"
	}
	in.OperationID = op.String()
	records := []sourcedata.Record{}
	if in.Action == "snapshot" {
		records, e = privatelists.ParseCSV(in.CSV)
		if e != nil {
			return Receipt{}, ErrInput
		}
	} else if in.Action != "withdraw" || in.CSV != "" {
		return Receipt{}, ErrInput
	}
	for i := range records {
		if !spec.ValidReference(records[i].SourceReference) || (records[i].Type != "Person" && records[i].Type != "Organization") {
			return Receipt{}, ErrInput
		}
		records[i].SourceType = spec.Category
		records[i].Properties["sourceStatus"] = []string{"Reviewed notice; " + in.NoticeID + "; no inference of guilt or complete agency coverage"}
	}
	attachmentHash, csvHash := digest(attachment), digest([]byte(in.CSV))
	request, _ := json.Marshal([]any{in, attachmentHash, p.Subject, p.TenantID})
	requestHash := digest(request)
	tx, e := s.begin(ctx, p)
	if e != nil {
		return Receipt{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, p.Subject+":"+op.String()); e != nil {
		return Receipt{}, e
	}
	var existing uuid.UUID
	var oldHash string
	e = tx.QueryRow(ctx, `SELECT id,request_hash FROM screening.reviewed_notice_imports WHERE created_by=$1 AND operation_id=$2`, p.Subject, op).Scan(&existing, &oldHash)
	if e == nil {
		if oldHash != requestHash {
			return Receipt{}, ErrConflict
		}
		tx.Rollback(ctx)
		return s.Get(ctx, p, existing)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return Receipt{}, e
	}
	// Register once, without adopting another connector or granting any tenant.
	var source string
	e = tx.QueryRow(ctx, `SELECT source_key FROM screening.reviewed_notice_datasets WHERE dataset_id=$1`, in.Dataset).Scan(&source)
	if errors.Is(e, pgx.ErrNoRows) {
		if _, e = tx.Exec(ctx, `INSERT INTO screening.source_datasets(owner_scope,id,label,category) VALUES($1,$2,$3,$4)`, uuid.Nil, in.Dataset, spec.Label, spec.Category); e != nil {
			return Receipt{}, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO screening.reviewed_notice_datasets(dataset_id,source_key,created_by,created_tenant) VALUES($1,$2,$3,$4)`, in.Dataset, in.Source, p.Subject, p.TenantID); e != nil {
			return Receipt{}, e
		}
		metadata, _ := json.Marshal(map[string]any{"coverage": spec.Coverage, "reference": spec.Reference, "parser": "reviewed-notice-csv-v1", "verification_method": "operator-reviewed upload; no automatic agency check", "rights_note": "Source-specific acquisition/redistribution approval is separate from content approval"})
		if _, e = tx.Exec(ctx, `INSERT INTO screening.source_connections(owner_scope,dataset_id,connector,config) VALUES($1,$2,$3,$4)`, uuid.Nil, in.Dataset, in.Source, metadata); e != nil {
			return Receipt{}, e
		}
	} else if e != nil {
		return Receipt{}, e
	} else if source != in.Source {
		return Receipt{}, ErrConflict
	}
	id := uuid.New()
	for _, artifact := range []struct {
		hash  string
		body  []byte
		media string
	}{{attachmentHash, attachment, media}, {csvHash, []byte(in.CSV), "text/csv"}} {
		if _, e = tx.Exec(ctx, `INSERT INTO screening.source_artifacts(owner_scope,dataset_id,sha256,body,media_type) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, uuid.Nil, in.Dataset, artifact.hash, artifact.body, artifact.media); e != nil {
			return Receipt{}, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO screening.source_retention_holds(id,owner_scope,dataset_id,reference_id,sha256,reason) VALUES($1,$2,$3,$4,$5,'reviewed-notice-evidence')`, uuid.New(), uuid.Nil, in.Dataset, id, artifact.hash); e != nil {
			return Receipt{}, e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO screening.reviewed_notice_imports(id,dataset_id,operation_id,created_by,created_tenant,action,status,notice_id,reference,publication_date,rights_review_reference,request_hash,attachment_sha256,csv_sha256) VALUES($1,$2,$3,$4,$5,$6,'staging',$7,$8,$9::date,$10,$11,$12,$13)`, id, in.Dataset, op, p.Subject, p.TenantID, in.Action, in.NoticeID, in.Reference, in.PublicationDate, in.RightsReference, requestHash, attachmentHash, csvHash); e != nil {
		return Receipt{}, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO screening.reviewed_notice_audit(import_id,dataset_id,actor,actor_tenant,action) VALUES($1,$2,$3,$4,'submitted')`, id, in.Dataset, p.Subject, p.TenantID); e != nil {
		return Receipt{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Receipt{}, e
	}
	ingest := s.store(p)
	ingest.AllowEmpty = in.Action == "withdraw"
	ingest.OnStaging = func(ctx context.Context, tx pgx.Tx, generation uuid.UUID) error {
		tag, e := tx.Exec(ctx, `UPDATE screening.reviewed_notice_imports SET generation=$2 WHERE id=$1 AND status='staging'`, id, generation)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		_, e = tx.Exec(ctx, `INSERT INTO screening.source_retention_holds(id,owner_scope,dataset_id,reference_id,generation,reason) VALUES($1,$2,$3,$4,$5,'pending-notice-review')`, uuid.New(), uuid.Nil, in.Dataset, id, generation)
		return e
	}
	envelope, _ := json.Marshal(struct {
		Version int                 `json:"version"`
		Source  string              `json:"source"`
		Parser  string              `json:"parser"`
		Records []sourcedata.Record `json:"records"`
	}{2, in.Source, "reviewed-notice-csv-v1", records})
	generation, e := ingest.Stage(ctx, sourceingest.Key{Dataset: in.Dataset}, bytes.NewReader(envelope))
	if e != nil {
		clean, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = s.DB.Exec(clean, `UPDATE screening.reviewed_notice_imports SET status='failed' WHERE id=$1 AND status='staging'`, id)
		return Receipt{}, e
	}
	finish, e := s.begin(ctx, p)
	if e != nil {
		return Receipt{}, e
	}
	defer finish.Rollback(ctx)
	tag, e := finish.Exec(ctx, `UPDATE screening.reviewed_notice_imports i SET status='pending_review',generation=$2,content_hash=v.content_hash,record_count=v.record_count FROM screening.source_versions v WHERE i.id=$1 AND i.status='staging' AND(v.owner_scope,v.dataset_id,v.id)=($3,i.dataset_id,$2) AND v.status='staged'`, id, generation, uuid.Nil)
	if e != nil {
		return Receipt{}, e
	}
	if tag.RowsAffected() != 1 {
		return Receipt{}, ErrConflict
	}
	if e = finish.Commit(ctx); e != nil {
		return Receipt{}, e
	}
	return s.Get(ctx, p, id)
}

func (s Service) Get(ctx context.Context, p identity.Principal, id uuid.UUID) (Receipt, error) {
	if s.DB == nil {
		return Receipt{}, ErrAuthority
	}
	if e := s.authorize(ctx, s.DB, p, false); e != nil {
		return Receipt{}, e
	}
	out := Receipt{}
	e := s.DB.QueryRow(ctx, `SELECT id,dataset_id,status,created_by,generation,published_generation,coalesce(content_hash,''),record_count FROM screening.reviewed_notice_imports WHERE id=$1`, id).Scan(&out.ID, &out.Dataset, &out.Status, &out.CreatedBy, &out.Generation, &out.PublishedGeneration, &out.Hash, &out.Count)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return out, e
}
func (s Service) Approve(ctx context.Context, p identity.Principal, id uuid.UUID) (Receipt, error) {
	item, e := s.Get(ctx, p, id)
	if e != nil {
		return Receipt{}, e
	}
	if item.CreatedBy.String() == p.Subject {
		return Receipt{}, ErrAuthority
	}
	if item.Status == "published" {
		return item, nil
	}
	if item.Status != "pending_review" || item.Generation == nil {
		return Receipt{}, ErrConflict
	}
	ingest := s.store(p)
	guard := func(ctx context.Context, tx pgx.Tx) error {
		var creator, generation uuid.UUID
		var hash, status string
		e := tx.QueryRow(ctx, `SELECT created_by,generation,content_hash,status FROM screening.reviewed_notice_imports WHERE id=$1 FOR UPDATE`, id).Scan(&creator, &generation, &hash, &status)
		if e != nil {
			return e
		}
		if creator.String() == p.Subject {
			return ErrAuthority
		}
		if generation != *item.Generation || hash != item.Hash || status != "pending_review" {
			return ErrConflict
		}
		return nil
	}
	ingest.Finalize = func(ctx context.Context, tx pgx.Tx, result sourceingest.Publication) error {
		if _, e := tx.Exec(ctx, `UPDATE screening.reviewed_notice_imports SET status='published',published_generation=$2,reviewed_by=$3,reviewed_tenant=$4,reviewed_at=now() WHERE id=$1`, id, result.Generation, p.Subject, p.TenantID); e != nil {
			return e
		}
		// Content review is NOT a rights approval or tenant grant. A previous explicit
		// rights approval remains, and freshness means review time, not a web fetch.
		if _, e := tx.Exec(ctx, `UPDATE screening.source_connections SET enabled=false,verified_generation=$2,last_success_at=now(),last_error='',lease_token=NULL,lease_until=NULL,fence=fence+1 WHERE owner_scope=$3 AND dataset_id=$1`, item.Dataset, result.Generation, uuid.Nil); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO screening.reviewed_notice_audit(import_id,dataset_id,actor,actor_tenant,action,detail) VALUES($1,$2,$3,$4,'approved',jsonb_build_object('generation',$5::text,'content_hash',$6::text))`, id, item.Dataset, p.Subject, p.TenantID, result.Generation.String(), item.Hash)
		return e
	}
	if _, e = ingest.PublishGuarded(ctx, sourceingest.Key{Dataset: item.Dataset}, *item.Generation, guard); e != nil {
		return Receipt{}, e
	}
	return s.Get(ctx, p, id)
}
func (s Service) Reject(ctx context.Context, p identity.Principal, id uuid.UUID) error {
	tx, e := s.begin(ctx, p)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var creator uuid.UUID
	var dataset, status string
	if e = tx.QueryRow(ctx, `SELECT created_by,dataset_id,status FROM screening.reviewed_notice_imports WHERE id=$1 FOR UPDATE`, id).Scan(&creator, &dataset, &status); e != nil {
		return e
	}
	if creator.String() == p.Subject {
		return ErrAuthority
	}
	if status == "rejected" {
		return nil
	}
	if status != "pending_review" {
		return ErrConflict
	}
	if _, e = tx.Exec(ctx, `UPDATE screening.reviewed_notice_imports SET status='rejected',reviewed_by=$2,reviewed_tenant=$3,reviewed_at=now() WHERE id=$1`, id, p.Subject, p.TenantID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM screening.source_retention_holds WHERE reference_id=$1`, id); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO screening.reviewed_notice_audit(import_id,dataset_id,actor,actor_tenant,action) VALUES($1,$2,$3,$4,'rejected')`, id, dataset, p.Subject, p.TenantID); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
