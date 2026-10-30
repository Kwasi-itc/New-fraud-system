// Package mysqlruntime composes the isolated MySQL ingestion runtime.
// Default deployment continues to use PostgreSQL/River.
package mysqlruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type Service struct {
	DB     *sql.DB
	Config Config
}

func (s *Service) owner(ctx context.Context, tenant uuid.UUID, name string) (*store.KeyedRecordOwner, error) {
	if tenant != s.Config.TenantID || name != s.Config.ObjectType {
		return nil, service.ErrForbidden
	}
	return store.ResolveRecordOwner(ctx, s.DB, s.Config.Database, tenant, name, s.Config.Token, s.Config.Limits, s.Config.ContractBytes)
}
func boundary(err error) error {
	if err == nil {
		return nil
	}
	kind := service.ErrUnavailable
	var uncertain *mysqlstore.CommitError
	switch {
	case errors.As(err, &uncertain):
		kind = service.ErrCommitUnknown
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, store.ErrOwnerAuthorization), errors.Is(err, service.ErrForbidden):
		kind = service.ErrForbidden
	case errors.Is(err, store.ErrReceiptConflict), errors.Is(err, store.ErrWorkIdentity):
		kind = service.ErrIdempotencyKeyReused
	case errors.Is(err, store.ErrRecordConflict):
		kind = service.ErrRecordConflict
	case errors.Is(err, store.ErrReceiptLimit), errors.Is(err, store.ErrContractBounds):
		kind = service.ErrRequestLimits
	case errors.Is(err, store.ErrCommandInput):
		kind = service.ErrBadInput
	case errors.Is(err, sql.ErrNoRows):
		kind = service.ErrResourceMissing
	}
	return &service.BoundaryError{Kind: kind, Cause: err}
}
func (s *Service) execute(ctx context.Context, tenant uuid.UUID, name string, mode ingestion.Mode, kind string, records []map[string]any, key *string) (store.ReceiptResult, []ingestion.ValidationError, error) {
	if key != nil && (len(*key) == 0 || len(*key) > s.Config.Limits.Receipt.KeyBytes || !utf8.ValidString(*key)) {
		return store.ReceiptResult{}, nil, service.ErrRequestLimits
	}
	owner, err := s.owner(ctx, tenant, name)
	if err != nil {
		return store.ReceiptResult{}, nil, boundary(err)
	}
	privateID := uuid.New()
	result, err := owner.ExecuteRequest(ctx, mode, kind, records, key, privateID)
	var validation *store.RecordValidationError
	if errors.As(err, &validation) && err == validation {
		return store.ReceiptResult{}, validation.Violations, nil
	}
	// No automatic retry generates a fresh identity after ambiguous commit.
	return result, nil, boundary(err)
}
func (s *Service) Ingest(ctx context.Context, in service.IngestInput) (ingestion.RecordResult, []ingestion.ValidationError, error) {
	r, v, e := s.execute(ctx, in.TenantID, in.ObjectType, in.Mode, "single", []map[string]any{in.Payload}, in.IdempotencyKey)
	if e != nil || len(v) > 0 {
		return ingestion.RecordResult{}, v, e
	}
	var out ingestion.RecordResult
	if e = json.Unmarshal(r.Payload, &out); e != nil {
		return ingestion.RecordResult{}, nil, boundary(e)
	}
	out.Replayed = r.Replayed
	return out, nil, nil
}
func (s *Service) BatchIngest(ctx context.Context, in service.BatchIngestInput) ([]ingestion.RecordResult, []ingestion.ValidationError, error) {
	r, v, e := s.execute(ctx, in.TenantID, in.ObjectType, in.Mode, "batch", in.Records, in.IdempotencyKey)
	if e != nil || len(v) > 0 {
		return nil, v, e
	}
	var out []ingestion.RecordResult
	if e = json.Unmarshal(r.Payload, &out); e != nil {
		return nil, nil, boundary(e)
	}
	for i := range out {
		out[i].Replayed = r.Replayed
	}
	return out, nil, nil
}
func (s *Service) work(ctx context.Context, tenant uuid.UUID, name string) (*store.WorkOwner, error) {
	owner, err := s.owner(ctx, tenant, name)
	if err != nil {
		return nil, boundary(err)
	}
	w, err := store.NewWorkOwner(owner, s.Config.UploadQueue, s.Config.DeferredQueue, s.Config.BodyBytes, s.Config.Attempts)
	return w, boundary(err)
}
func (s *Service) submit(ctx context.Context, tenant uuid.UUID, name string, in store.WorkInput) (store.UploadSummary, error) {
	if len(in.Payload) > s.Config.BodyBytes {
		return store.UploadSummary{}, service.ErrRequestLimits
	}
	w, e := s.work(ctx, tenant, name)
	if e != nil {
		return store.UploadSummary{}, e
	}
	if _, e = w.Create(ctx, in); e != nil {
		return store.UploadSummary{}, boundary(e)
	}
	r, e := w.Detail(ctx, in.ID, in.Kind)
	return r, boundary(e)
}
func status(state string, upload bool) string {
	switch state {
	case "available":
		if upload {
			return "pending"
		}
		return "queued"
	case "running":
		return "processing"
	case "completed":
		return "completed"
	default:
		return "failed"
	}
}

type Uploads struct{ *Service }
type Deferred struct{ *Service }

func (s Uploads) adapt(r store.UploadSummary) ingestion.UploadLog {
	return ingestion.UploadLog{ID: r.ID.String(), TenantID: s.Config.TenantID.String(), ObjectType: s.Config.ObjectType, Mode: r.Mode, Filename: r.Filename, ContentType: r.ContentType, Status: ingestion.UploadLogStatus(status(r.State, true)), TotalRows: r.TotalRows, SuccessfulRows: r.SuccessfulRows, FailedRows: r.FailedRows, AttemptCount: r.Attempt, ErrorMessage: r.ErrorCode, RequestedAt: r.RequestedAt, StartedAt: r.StartedAt, CompletedAt: r.CompletedAt}
}
func (s Deferred) adapt(r store.UploadSummary) ingestion.DeferredIngest {
	return ingestion.DeferredIngest{ID: r.ID.String(), TenantID: s.Config.TenantID.String(), ObjectType: s.Config.ObjectType, Mode: r.Mode, Status: ingestion.DeferredIngestStatus(status(r.State, false)), AttemptCount: r.Attempt, ErrorMessage: r.ErrorCode, RequestedAt: r.RequestedAt, StartedAt: r.StartedAt, CompletedAt: r.CompletedAt}
}
func (s Uploads) Create(ctx context.Context, tenant uuid.UUID, name string, mode ingestion.Mode, filename, content string, payload []byte) (ingestion.UploadLog, error) {
	r, e := s.submit(ctx, tenant, name, store.WorkInput{ID: uuid.New(), Kind: store.UploadWork, Mode: mode, Payload: payload, Filename: filename, ContentType: content, ScheduledAt: time.Now().UTC().Truncate(time.Microsecond)})
	if e != nil {
		return ingestion.UploadLog{}, e
	}
	return s.adapt(r), nil
}
func (s Deferred) Create(ctx context.Context, tenant uuid.UUID, name string, mode ingestion.Mode, payload map[string]any, key *string) (ingestion.DeferredIngest, error) {
	if payload == nil {
		return ingestion.DeferredIngest{}, service.ErrBadInput
	}
	wire, e := json.Marshal(payload)
	if e != nil {
		return ingestion.DeferredIngest{}, service.ErrBadInput
	}
	r, e := s.submit(ctx, tenant, name, store.WorkInput{ID: uuid.New(), Kind: store.DeferredWork, Mode: mode, Payload: wire, RequestKey: key, ScheduledAt: time.Now().UTC().Truncate(time.Microsecond)})
	if e != nil {
		return ingestion.DeferredIngest{}, e
	}
	return s.adapt(r), nil
}
func (s Uploads) Get(ctx context.Context, id uuid.UUID) (ingestion.UploadLog, error) {
	w, e := s.work(ctx, s.Config.TenantID, s.Config.ObjectType)
	if e != nil {
		return ingestion.UploadLog{}, e
	}
	r, e := w.Detail(ctx, id, store.UploadWork)
	if e != nil {
		return ingestion.UploadLog{}, boundary(e)
	}
	return s.adapt(r), nil
}
func (s Deferred) Get(ctx context.Context, id uuid.UUID) (ingestion.DeferredIngest, error) {
	w, e := s.work(ctx, s.Config.TenantID, s.Config.ObjectType)
	if e != nil {
		return ingestion.DeferredIngest{}, e
	}
	r, e := w.Detail(ctx, id, store.DeferredWork)
	if e != nil {
		return ingestion.DeferredIngest{}, boundary(e)
	}
	return s.adapt(r), nil
}
func (s Uploads) List(ctx context.Context, tenant uuid.UUID, name string) ([]ingestion.UploadLog, error) {
	w, e := s.work(ctx, tenant, name)
	if e != nil {
		return nil, e
	}
	page, e := w.ListUploads(ctx, 100, nil)
	if e != nil {
		return nil, boundary(e)
	}
	out := make([]ingestion.UploadLog, len(page.Items))
	for i, r := range page.Items {
		out[i] = s.adapt(r)
	}
	return out, nil
}
