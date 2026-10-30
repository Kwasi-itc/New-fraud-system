package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/google/uuid"
)

type recordingUpload struct {
	ports.UploadLogRepository
	saved ingestion.UploadLog
	err   error
}

func (r *recordingUpload) Update(_ context.Context, log ingestion.UploadLog) error {
	r.saved = log
	return r.err
}

type recordingDeferred struct {
	ports.DeferredIngestRepository
	saved ingestion.DeferredIngest
	err   error
}

func (r *recordingDeferred) Update(_ context.Context, job ingestion.DeferredIngest) error {
	r.saved = job
	return r.err
}

func TestQueueCreationRequiresTransaction(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	clock := fixedClock{now: time.Now().UTC()}
	ids := &fixedIDGenerator{next: []uuid.UUID{uuid.New(), uuid.New()}}
	upload := NewUploadLogService(nil, IngestService{}, nil, ids, clock, 2)
	if _, err := upload.Create(ctx, tenant, "transactions", ingestion.ModeCreate, "test.csv", "text/csv", []byte("object_id\n1")); err == nil {
		t.Fatal("upload accepted without atomic queue boundary")
	}
	deferred := NewDeferredIngestService(nil, IngestService{}, nil, ids, clock, 2)
	if _, err := deferred.Create(ctx, tenant, "transactions", ingestion.ModeCreate, map[string]any{"object_id": "1"}, nil); err == nil {
		t.Fatal("deferred accepted without atomic queue boundary")
	}
}

func TestRetryableFailureIsReturnedToWorker(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	uploadRepo := &recordingUpload{}
	upload := NewUploadLogService(uploadRepo, IngestService{}, nil, nil, nil, 2)
	log := ingestion.UploadLog{ID: uuid.NewString(), AttemptCount: 1, StartedAt: &now}
	if err := upload.handleRetryableFailure(ctx, &log, now, "temporary failure"); err == nil {
		t.Fatal("retryable upload acknowledged as success")
	}
	if uploadRepo.saved.Status != ingestion.UploadLogStatusUploaded || uploadRepo.saved.StartedAt != nil || uploadRepo.saved.CompletedAt != nil {
		t.Fatalf("wrong retry state: %+v", uploadRepo.saved)
	}
	log.AttemptCount = 2
	if err := upload.handleRetryableFailure(ctx, &log, now, "terminal failure"); err != nil {
		t.Fatal(err)
	}
	if uploadRepo.saved.Status != ingestion.UploadLogStatusFailed || uploadRepo.saved.CompletedAt == nil {
		t.Fatal("terminal upload state missing")
	}
	deferredRepo := &recordingDeferred{}
	deferred := NewDeferredIngestService(deferredRepo, IngestService{}, nil, nil, nil, 2)
	execution := ingestion.DeferredIngest{ID: uuid.NewString(), AttemptCount: 1, StartedAt: &now}
	if err := deferred.handleRetryableFailure(ctx, &execution, now, "temporary failure"); err == nil {
		t.Fatal("retryable deferred job acknowledged as success")
	}
	if deferredRepo.saved.Status != ingestion.DeferredIngestStatusQueued || deferredRepo.saved.StartedAt != nil || deferredRepo.saved.CompletedAt != nil {
		t.Fatalf("wrong retry state: %+v", deferredRepo.saved)
	}
	execution.AttemptCount = 2
	if err := deferred.handleRetryableFailure(ctx, &execution, now, "terminal failure"); err != nil {
		t.Fatal(err)
	}
	if deferredRepo.saved.Status != ingestion.DeferredIngestStatusFailed || deferredRepo.saved.CompletedAt == nil {
		t.Fatal("terminal deferred state missing")
	}
	writeError := errors.New("state update failed")
	uploadRepo.err = writeError
	deferredRepo.err = writeError
	log.AttemptCount = 1
	execution.AttemptCount = 1
	if err := upload.handleRetryableFailure(ctx, &log, now, "temporary"); !errors.Is(err, writeError) {
		t.Fatal("lost persistence error", err)
	}
	if err := deferred.handleRetryableFailure(ctx, &execution, now, "temporary"); !errors.Is(err, writeError) {
		t.Fatal("lost persistence error", err)
	}
}
