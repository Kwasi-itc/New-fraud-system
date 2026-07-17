package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/riverjobs"
)

type ScreeningService struct {
	txManager         ports.TransactionManager
	idGen             ports.IDGenerator
	clock             ports.Clock
	screeningRepo     ports.ScreeningRepository
	matchRepo         ports.ScreeningMatchRepository
	commentRepo       ports.ScreeningCommentRepository
	whitelistRepo     ports.ScreeningWhitelistRepository
	fileRepo          ports.ScreeningFileRepository
	continuousRepo    ports.ContinuousConfigRepository
	monitoredObjRepo  ports.MonitoredObjectRepository
	datasetJobRepo    ports.DatasetUpdateJobRepository
	provider          ports.ScreeningProvider
	inboxReader       ports.InboxReader
	casePublisher     ports.CasePublisher
	blobStore         ports.BlobStore
	decisionPublisher ports.DecisionEnginePublisher
	enqueuer          riverjobs.ScreeningEnqueuer
	datasetEnqueuer   riverjobs.DatasetJobEnqueuer
	monitoredEnqueuer riverjobs.MonitoredObjectEnqueuer
}

func NewScreeningService(
	txManager ports.TransactionManager,
	idGen ports.IDGenerator,
	clock ports.Clock,
	screeningRepo ports.ScreeningRepository,
	matchRepo ports.ScreeningMatchRepository,
	commentRepo ports.ScreeningCommentRepository,
	whitelistRepo ports.ScreeningWhitelistRepository,
	fileRepo ports.ScreeningFileRepository,
	continuousRepo ports.ContinuousConfigRepository,
	monitoredObjRepo ports.MonitoredObjectRepository,
	datasetJobRepo ports.DatasetUpdateJobRepository,
	provider ports.ScreeningProvider,
	inboxReader ports.InboxReader,
	casePublisher ports.CasePublisher,
	blobStore ports.BlobStore,
	decisionPublisher ports.DecisionEnginePublisher,
	enqueuer riverjobs.ScreeningEnqueuer,
	datasetEnqueuer riverjobs.DatasetJobEnqueuer,
	monitoredEnqueuer riverjobs.MonitoredObjectEnqueuer,
) ScreeningService {
	if enqueuer == nil {
		enqueuer = riverjobs.NoopScreeningEnqueuer{}
	}
	if datasetEnqueuer == nil {
		datasetEnqueuer = riverjobs.NoopDatasetJobEnqueuer{}
	}
	if monitoredEnqueuer == nil {
		monitoredEnqueuer = riverjobs.NoopMonitoredObjectEnqueuer{}
	}
	return ScreeningService{
		txManager:         txManager,
		idGen:             idGen,
		clock:             clock,
		screeningRepo:     screeningRepo,
		matchRepo:         matchRepo,
		commentRepo:       commentRepo,
		whitelistRepo:     whitelistRepo,
		fileRepo:          fileRepo,
		continuousRepo:    continuousRepo,
		monitoredObjRepo:  monitoredObjRepo,
		datasetJobRepo:    datasetJobRepo,
		provider:          provider,
		inboxReader:       inboxReader,
		casePublisher:     casePublisher,
		blobStore:         blobStore,
		decisionPublisher: decisionPublisher,
		enqueuer:          enqueuer,
		datasetEnqueuer:   datasetEnqueuer,
		monitoredEnqueuer: monitoredEnqueuer,
	}
}

func (s ScreeningService) CreateScreening(ctx context.Context, tenantID string, request screening.SearchRequest) (screening.Screening, error) {
	if len(request.Queries) == 0 {
		return screening.Screening{}, fmt.Errorf("queries are required")
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return screening.Screening{}, fmt.Errorf("marshal request: %w", err)
	}

	now := s.clock.Now()
	item := screening.Screening{
		ID:                           s.idGen.New().String(),
		TenantID:                     tenantID,
		DecisionID:                   request.DecisionID,
		ScenarioID:                   request.ScenarioID,
		ScreeningConfigID:            request.ScreeningConfigID,
		IdempotencyKey:               request.IdempotencyKey,
		Provider:                     request.Provider,
		ObjectType:                   request.ObjectType,
		ObjectID:                     request.ObjectID,
		Status:                       screening.StatusPending,
		RequestJSON:                  requestJSON,
		ResponseJSON:                 json.RawMessage(`{}`),
		IsManual:                     request.IsManual,
		UniqueCounterpartyIdentifier: request.UniqueCounterpartyIdentifier,
		CreatedAt:                    now,
		UpdatedAt:                    now,
	}
	if err := item.Validate(); err != nil {
		return screening.Screening{}, err
	}

	var created screening.Screening
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if item.IdempotencyKey != "" {
			existing, lookupErr := store.Screenings().GetByIdempotencyKey(ctx, tenantID, item.IdempotencyKey)
			if lookupErr == nil {
				created = existing
				return nil
			}
		}
		var runErr error
		created, runErr = store.Screenings().Create(ctx, item)
		if runErr != nil {
			return runErr
		}
		return s.enqueuer.EnqueueTx(ctx, store.RawTx(), created.TenantID, created.ID, nil)
	})
	return created, err
}

func (s ScreeningService) CreateFreeformScreening(ctx context.Context, tenantID string, request screening.SearchRequest) (screening.Screening, error) {
	request.IsManual = true
	return s.CreateScreening(ctx, tenantID, request)
}

func (s ScreeningService) ListByDecision(ctx context.Context, tenantID, decisionID string) ([]screening.Screening, error) {
	return s.screeningRepo.ListByDecision(ctx, tenantID, decisionID)
}

func (s ScreeningService) GetDetails(ctx context.Context, tenantID, screeningID string) (screening.Details, error) {
	item, err := s.screeningRepo.GetByID(ctx, tenantID, screeningID)
	if err != nil {
		return screening.Details{}, err
	}

	matches, err := s.matchRepo.ListByScreening(ctx, tenantID, screeningID)
	if err != nil {
		return screening.Details{}, err
	}
	if len(matches) > 0 {
		matchIDs := make([]string, 0, len(matches))
		index := make(map[string]int, len(matches))
		for i := range matches {
			matchIDs = append(matchIDs, matches[i].ID)
			index[matches[i].ID] = i
		}
		comments, err := s.commentRepo.ListByMatchIDs(ctx, tenantID, matchIDs)
		if err != nil {
			return screening.Details{}, err
		}
		for _, comment := range comments {
			idx, ok := index[comment.MatchID]
			if !ok {
				continue
			}
			matches[idx].Comments = append(matches[idx].Comments, comment)
		}
	}

	return screening.Details{
		Screening: item,
		Matches:   matches,
	}, nil
}

func (s ScreeningService) Retry(ctx context.Context, tenantID, screeningID string) (screening.Screening, error) {
	item, err := s.screeningRepo.GetByID(ctx, tenantID, screeningID)
	if err != nil {
		return screening.Screening{}, err
	}
	if item.Status != screening.StatusFailed {
		return screening.Screening{}, fmt.Errorf("screening status %q cannot be retried", item.Status)
	}

	now := s.clock.Now()
	item.Status = screening.StatusPending
	item.LastError = ""
	item.ProviderReference = ""
	item.ResponseJSON = json.RawMessage(`{}`)
	item.UpdatedAt = now
	item.SentAt = nil
	item.CompletedAt = nil
	item.FailedAt = nil

	var updated screening.Screening
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.ScreeningMatches().ReplaceForScreening(ctx, item.ID, nil); err != nil {
			return err
		}
		var runErr error
		updated, runErr = store.Screenings().Update(ctx, item)
		if runErr != nil {
			return runErr
		}
		return s.enqueuer.EnqueueTx(ctx, store.RawTx(), updated.TenantID, updated.ID, nil)
	})
	return updated, err
}

func (s ScreeningService) ReviewMatch(ctx context.Context, tenantID, matchID, status, comment, reviewerID string, whitelist bool) (screening.Match, error) {
	match, err := s.matchRepo.GetByID(ctx, tenantID, matchID)
	if err != nil {
		return screening.Match{}, err
	}

	nextStatus := screening.MatchStatus(status)
	if !nextStatus.IsValid() {
		return screening.Match{}, fmt.Errorf("invalid match status %q", status)
	}
	if nextStatus != screening.MatchStatusConfirmedHit && nextStatus != screening.MatchStatusNoHit {
		return screening.Match{}, fmt.Errorf("match status %q is not reviewable", status)
	}

	now := s.clock.Now()
	match.Status = nextStatus
	match.UpdatedAt = now

	var updated screening.Match
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.ScreeningMatches().Update(ctx, match)
		if runErr != nil {
			return runErr
		}

		if comment != "" {
			_, runErr = store.ScreeningComments().Create(ctx, screening.Comment{
				ID:          s.idGen.New().String(),
				TenantID:    tenantID,
				MatchID:     match.ID,
				CommentText: comment,
				AuthorID:    reviewerID,
				CreatedAt:   now,
			})
			if runErr != nil {
				return runErr
			}
		}

		item, runErr := store.Screenings().GetByID(ctx, tenantID, match.ScreeningID)
		if runErr != nil {
			return runErr
		}

		switch nextStatus {
		case screening.MatchStatusConfirmedHit:
			item.Status = screening.StatusConfirmedHit
		case screening.MatchStatusNoHit:
			pendingCount, countErr := store.ScreeningMatches().CountPendingByScreening(ctx, match.ScreeningID)
			if countErr != nil {
				return countErr
			}
			if pendingCount == 0 {
				item.Status = screening.StatusNoHit
			}
		}
		item.UpdatedAt = now
		if item.Status == screening.StatusConfirmedHit || item.Status == screening.StatusNoHit {
			item.CompletedAt = &now
		}
		if _, runErr = store.Screenings().Update(ctx, item); runErr != nil {
			return runErr
		}

		if whitelist && nextStatus == screening.MatchStatusNoHit {
			_, runErr = store.Whitelist().Create(ctx, screening.WhitelistEntry{
				ID:                           s.idGen.New().String(),
				TenantID:                     tenantID,
				EntityID:                     match.EntityID,
				UniqueCounterpartyIdentifier: match.UniqueCounterpartyIdentifier,
				ReviewerID:                   reviewerID,
				CreatedAt:                    now,
			})
			if runErr != nil {
				return runErr
			}
		}

		return nil
	})
	if err == nil {
		_ = s.publishScreeningStatusChanged(ctx, tenantID, match.ScreeningID)
	}
	if err == nil && s.casePublisher != nil {
		screeningItem, getErr := s.screeningRepo.GetByID(ctx, tenantID, match.ScreeningID)
		if getErr == nil {
			_ = s.casePublisher.PublishScreeningReviewed(ctx, ports.ScreeningReviewedCommand{
				TenantID:    tenantID,
				ScreeningID: screeningItem.ID,
				DecisionID:  screeningItem.DecisionID,
				MatchID:     match.ID,
				Status:      string(nextStatus),
				ReviewerID:  reviewerID,
			})
		}
	}
	return updated, err
}

func (s ScreeningService) AddComment(ctx context.Context, tenantID, matchID, comment, authorID string) (screening.Comment, error) {
	if _, err := s.matchRepo.GetByID(ctx, tenantID, matchID); err != nil {
		return screening.Comment{}, err
	}
	item := screening.Comment{
		ID:          s.idGen.New().String(),
		TenantID:    tenantID,
		MatchID:     matchID,
		CommentText: comment,
		AuthorID:    authorID,
		CreatedAt:   s.clock.Now(),
	}
	if item.CommentText == "" {
		return screening.Comment{}, fmt.Errorf("comment is required")
	}

	var created screening.Comment
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		created, runErr = store.ScreeningComments().Create(ctx, item)
		return runErr
	})
	return created, err
}

func (s ScreeningService) CreateWhitelistEntry(ctx context.Context, tenantID, entityID, reviewerID string, counterpartyIdentifier *string) (screening.WhitelistEntry, error) {
	item := screening.WhitelistEntry{
		ID:                           s.idGen.New().String(),
		TenantID:                     tenantID,
		EntityID:                     entityID,
		UniqueCounterpartyIdentifier: counterpartyIdentifier,
		ReviewerID:                   reviewerID,
		CreatedAt:                    s.clock.Now(),
	}
	if item.EntityID == "" {
		return screening.WhitelistEntry{}, fmt.Errorf("entity_id is required")
	}

	var created screening.WhitelistEntry
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		created, runErr = store.Whitelist().Create(ctx, item)
		return runErr
	})
	return created, err
}

func (s ScreeningService) DeleteWhitelistEntry(ctx context.Context, tenantID, entityID string, counterpartyIdentifier *string) error {
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.Whitelist().Delete(ctx, tenantID, entityID, counterpartyIdentifier)
	})
}

func (s ScreeningService) SearchWhitelist(ctx context.Context, tenantID string, entityID, counterpartyIdentifier *string) ([]screening.WhitelistEntry, error) {
	return s.whitelistRepo.Search(ctx, tenantID, entityID, counterpartyIdentifier)
}

func (s ScreeningService) EnrichMatch(ctx context.Context, tenantID, matchID string) (screening.Match, error) {
	match, err := s.matchRepo.GetByID(ctx, tenantID, matchID)
	if err != nil {
		return screening.Match{}, err
	}
	result, err := s.provider.Enrich(ctx, match.Provider, match.EntityID)
	if err != nil {
		return screening.Match{}, err
	}
	merged, err := mergePayloads(match.Payload, result.RawPayload)
	if err != nil {
		return screening.Match{}, err
	}
	match.Payload = merged
	match.Enriched = true
	match.UpdatedAt = s.clock.Now()

	var updated screening.Match
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.ScreeningMatches().Update(ctx, match)
		return runErr
	})
	return updated, err
}

func (s ScreeningService) CreateFile(ctx context.Context, tenantID, screeningID, fileName, contentType, storageKey, uploadedBy string, fileSize int64) (screening.File, error) {
	if _, err := s.screeningRepo.GetByID(ctx, tenantID, screeningID); err != nil {
		return screening.File{}, err
	}
	item := screening.File{
		ID:          s.idGen.New().String(),
		TenantID:    tenantID,
		ScreeningID: screeningID,
		FileName:    fileName,
		ContentType: contentType,
		FileSize:    fileSize,
		StorageKey:  storageKey,
		UploadedBy:  uploadedBy,
		CreatedAt:   s.clock.Now(),
	}
	if item.FileName == "" {
		return screening.File{}, fmt.Errorf("file_name is required")
	}

	var created screening.File
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		created, runErr = store.ScreeningFiles().Create(ctx, item)
		return runErr
	})
	if err == nil && s.casePublisher != nil {
		_ = s.casePublisher.PublishScreeningEvidenceUploaded(ctx, ports.ScreeningEvidenceUploadedCommand{
			TenantID:    tenantID,
			ScreeningID: screeningID,
			FileID:      created.ID,
			UploadedBy:  uploadedBy,
		})
	}
	return created, err
}

func (s ScreeningService) ListFiles(ctx context.Context, tenantID, screeningID string) ([]screening.File, error) {
	return s.fileRepo.ListByScreening(ctx, tenantID, screeningID)
}

func (s ScreeningService) GetFile(ctx context.Context, tenantID, screeningID, fileID string) (screening.File, error) {
	return s.fileRepo.GetByID(ctx, tenantID, screeningID, fileID)
}

func (s ScreeningService) CreateFileUpload(ctx context.Context, tenantID, screeningID, fileName, contentType, uploadedBy string, fileSize int64) (screening.File, ports.BlobUploadSession, error) {
	if s.blobStore == nil {
		return screening.File{}, ports.BlobUploadSession{}, fmt.Errorf("blob store is not configured")
	}
	session, err := s.blobStore.CreateUploadSession(ctx, tenantID, screeningID, fileName, contentType, fileSize)
	if err != nil {
		return screening.File{}, ports.BlobUploadSession{}, err
	}
	file, err := s.CreateFile(ctx, tenantID, screeningID, fileName, contentType, session.StorageKey, uploadedBy, fileSize)
	if err != nil {
		return screening.File{}, ports.BlobUploadSession{}, err
	}
	return file, session, nil
}

func (s ScreeningService) GetFileDownload(ctx context.Context, tenantID, screeningID, fileID string) (ports.BlobDownload, error) {
	if s.blobStore == nil {
		return ports.BlobDownload{}, fmt.Errorf("blob store is not configured")
	}
	file, err := s.GetFile(ctx, tenantID, screeningID, fileID)
	if err != nil {
		return ports.BlobDownload{}, err
	}
	return s.blobStore.GetDownloadURL(ctx, file.StorageKey)
}

func (s ScreeningService) CreateContinuousConfig(ctx context.Context, tenantID, name, objectType, provider string, fieldMapJSON json.RawMessage, reviewInboxID *string, enabled bool) (screening.ContinuousConfig, error) {
	if reviewInboxID != nil {
		if err := s.validateInbox(ctx, tenantID, *reviewInboxID); err != nil {
			return screening.ContinuousConfig{}, err
		}
	}
	now := s.clock.Now()
	item := screening.ContinuousConfig{
		ID:            s.idGen.New().String(),
		TenantID:      tenantID,
		Name:          name,
		ObjectType:    objectType,
		Provider:      provider,
		FieldMapJSON:  fieldMapJSON,
		ReviewInboxID: reviewInboxID,
		Enabled:       enabled,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := item.Validate(); err != nil {
		return screening.ContinuousConfig{}, err
	}
	var created screening.ContinuousConfig
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		created, runErr = store.ContinuousConfigs().Create(ctx, item)
		return runErr
	})
	return created, err
}

func (s ScreeningService) ListContinuousConfigs(ctx context.Context, tenantID string) ([]screening.ContinuousConfig, error) {
	return s.continuousRepo.ListByTenant(ctx, tenantID)
}

func (s ScreeningService) GetContinuousConfig(ctx context.Context, tenantID, configID string) (screening.ContinuousConfig, error) {
	return s.continuousRepo.GetByID(ctx, tenantID, configID)
}

func (s ScreeningService) UpdateContinuousConfig(ctx context.Context, tenantID, configID, name, objectType, provider string, fieldMapJSON json.RawMessage, reviewInboxID *string, enabled bool) (screening.ContinuousConfig, error) {
	if reviewInboxID != nil {
		if err := s.validateInbox(ctx, tenantID, *reviewInboxID); err != nil {
			return screening.ContinuousConfig{}, err
		}
	}
	item, err := s.continuousRepo.GetByID(ctx, tenantID, configID)
	if err != nil {
		return screening.ContinuousConfig{}, err
	}
	item.Name = name
	item.ObjectType = objectType
	item.Provider = provider
	item.FieldMapJSON = fieldMapJSON
	item.ReviewInboxID = reviewInboxID
	item.Enabled = enabled
	item.UpdatedAt = s.clock.Now()
	if err := item.Validate(); err != nil {
		return screening.ContinuousConfig{}, err
	}
	var updated screening.ContinuousConfig
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.ContinuousConfigs().Update(ctx, item)
		return runErr
	})
	return updated, err
}

func (s ScreeningService) DeleteContinuousConfig(ctx context.Context, tenantID, configID string) error {
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.ContinuousConfigs().Delete(ctx, tenantID, configID)
	})
}

func (s ScreeningService) CreateMonitoredObject(ctx context.Context, tenantID, configID, objectType, objectID string, attributesJSON json.RawMessage) (screening.MonitoredObject, error) {
	if _, err := s.continuousRepo.GetByID(ctx, tenantID, configID); err != nil {
		return screening.MonitoredObject{}, err
	}
	now := s.clock.Now()
	item := screening.MonitoredObject{
		ID:             s.idGen.New().String(),
		TenantID:       tenantID,
		ConfigID:       configID,
		ObjectType:     objectType,
		ObjectID:       objectID,
		Status:         screening.MonitoredObjectStatusPending,
		AttributesJSON: attributesJSON,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := item.Validate(); err != nil {
		return screening.MonitoredObject{}, err
	}
	var created screening.MonitoredObject
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		created, runErr = store.MonitoredObjects().Create(ctx, item)
		if runErr != nil {
			return runErr
		}
		return s.monitoredEnqueuer.EnqueueTx(ctx, store.RawTx(), created.TenantID, created.ID, nil)
	})
	return created, err
}

func (s ScreeningService) ListMonitoredObjects(ctx context.Context, tenantID, configID string) ([]screening.MonitoredObject, error) {
	return s.monitoredObjRepo.ListByConfig(ctx, tenantID, configID)
}

func (s ScreeningService) GetMonitoredObject(ctx context.Context, tenantID, monitoredObjectID string) (screening.MonitoredObject, error) {
	return s.monitoredObjRepo.GetByID(ctx, tenantID, monitoredObjectID)
}

func (s ScreeningService) DeleteMonitoredObject(ctx context.Context, tenantID, monitoredObjectID string) error {
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.MonitoredObjects().Delete(ctx, tenantID, monitoredObjectID)
	})
}

func (s ScreeningService) RequeueMonitoredObject(ctx context.Context, tenantID, monitoredObjectID string, attributesJSON json.RawMessage) (screening.MonitoredObject, error) {
	item, err := s.monitoredObjRepo.GetByID(ctx, tenantID, monitoredObjectID)
	if err != nil {
		return screening.MonitoredObject{}, err
	}
	item.Status = screening.MonitoredObjectStatusPending
	item.UpdatedAt = s.clock.Now()
	if len(attributesJSON) > 0 {
		item.AttributesJSON = attributesJSON
	}
	var updated screening.MonitoredObject
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.MonitoredObjects().Update(ctx, item)
		if runErr != nil {
			return runErr
		}
		return s.monitoredEnqueuer.EnqueueTx(ctx, store.RawTx(), updated.TenantID, updated.ID, nil)
	})
	return updated, err
}

func (s ScreeningService) GetDatasetCatalog(ctx context.Context, providerName string) (screening.DatasetCatalog, error) {
	return s.provider.GetCatalog(ctx, providerName)
}

func (s ScreeningService) GetDatasetFreshness(ctx context.Context, providerName string) (screening.DatasetFreshness, error) {
	return s.provider.GetFreshness(ctx, providerName)
}

func (s ScreeningService) CreateDatasetUpdateJob(ctx context.Context, tenantID, providerName, jobType, cursor string) (screening.DatasetUpdateJob, error) {
	now := s.clock.Now()
	item := screening.DatasetUpdateJob{
		ID:         s.idGen.New().String(),
		TenantID:   tenantID,
		Provider:   providerName,
		JobType:    jobType,
		Status:     screening.DatasetUpdateJobStatusPending,
		Cursor:     cursor,
		ResultJSON: json.RawMessage(`{}`),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := item.Validate(); err != nil {
		return screening.DatasetUpdateJob{}, err
	}
	var created screening.DatasetUpdateJob
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		created, runErr = store.DatasetUpdateJobs().Create(ctx, item)
		if runErr != nil {
			return runErr
		}
		return s.datasetEnqueuer.EnqueueTx(ctx, store.RawTx(), created.TenantID, created.ID, nil)
	})
	return created, err
}

func (s ScreeningService) ListDatasetUpdateJobs(ctx context.Context, tenantID string) ([]screening.DatasetUpdateJob, error) {
	return s.datasetJobRepo.ListByTenant(ctx, tenantID)
}

func (s ScreeningService) GetDatasetUpdateJob(ctx context.Context, tenantID, jobID string) (screening.DatasetUpdateJob, error) {
	return s.datasetJobRepo.GetByID(ctx, tenantID, jobID)
}

func (s ScreeningService) RetryDatasetUpdateJob(ctx context.Context, tenantID, jobID string) (screening.DatasetUpdateJob, error) {
	item, err := s.datasetJobRepo.GetByID(ctx, tenantID, jobID)
	if err != nil {
		return screening.DatasetUpdateJob{}, err
	}
	if item.Status != screening.DatasetUpdateJobStatusFailed {
		return screening.DatasetUpdateJob{}, fmt.Errorf("dataset update job status %q cannot be retried", item.Status)
	}
	item.Status = screening.DatasetUpdateJobStatusPending
	item.LastError = ""
	item.UpdatedAt = s.clock.Now()
	item.StartedAt = nil
	item.CompletedAt = nil
	var updated screening.DatasetUpdateJob
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.DatasetUpdateJobs().Update(ctx, item)
		if runErr != nil {
			return runErr
		}
		return s.datasetEnqueuer.EnqueueTx(ctx, store.RawTx(), updated.TenantID, updated.ID, nil)
	})
	return updated, err
}

func (s ScreeningService) publishScreeningStatusChanged(ctx context.Context, tenantID, screeningID string) error {
	if s.decisionPublisher == nil {
		return nil
	}

	item, err := s.screeningRepo.GetByID(ctx, tenantID, screeningID)
	if err != nil {
		return err
	}
	matches, err := s.matchRepo.ListByScreening(ctx, tenantID, screeningID)
	if err != nil {
		return err
	}
	return s.decisionPublisher.PublishScreeningStatusChanged(ctx, ports.ScreeningStatusChangedCommand{
		TenantID:          item.TenantID,
		ScreeningID:       item.ID,
		DecisionID:        item.DecisionID,
		ScenarioID:        item.ScenarioID,
		ScreeningConfigID: item.ScreeningConfigID,
		Status:            string(item.Status),
		Provider:          item.Provider,
		ObjectType:        item.ObjectType,
		ObjectID:          item.ObjectID,
		ProviderReference: item.ProviderReference,
		LastError:         item.LastError,
		Partial:           item.Partial,
		IdempotencyKey:    item.IdempotencyKey,
		CompletedAt:       item.CompletedAt,
		MatchCount:        len(matches),
	})
}

func mergePayloads(originalRaw, newRaw []byte) ([]byte, error) {
	var original map[string]any
	if len(originalRaw) == 0 {
		original = map[string]any{}
	} else if err := json.Unmarshal(originalRaw, &original); err != nil {
		return nil, err
	}

	var incoming map[string]any
	if len(newRaw) == 0 {
		incoming = map[string]any{}
	} else if err := json.Unmarshal(newRaw, &incoming); err != nil {
		return nil, err
	}

	for k, v := range incoming {
		original[k] = v
	}
	return json.Marshal(original)
}

func (s ScreeningService) validateInbox(ctx context.Context, tenantID, inboxID string) error {
	if s.inboxReader == nil {
		return nil
	}
	inbox, err := s.inboxReader.GetInbox(ctx, tenantID, inboxID)
	if err != nil {
		return err
	}
	if inbox.TenantID != "" && inbox.TenantID != tenantID {
		return fmt.Errorf("inbox not found for the tenant")
	}
	if inbox.Status != "" && inbox.Status != "active" {
		return fmt.Errorf("inbox is not active")
	}
	return nil
}
