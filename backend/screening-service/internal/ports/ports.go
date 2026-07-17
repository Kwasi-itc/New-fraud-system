package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
)

type IDGenerator interface {
	New() uuid.UUID
}

type Clock interface {
	Now() time.Time
}

type TransactionManager interface {
	Run(ctx context.Context, fn func(store MutationStore) error) error
}

type MutationStore interface {
	Screenings() ScreeningRepository
	ScreeningMatches() ScreeningMatchRepository
	ScreeningComments() ScreeningCommentRepository
	Whitelist() ScreeningWhitelistRepository
	ScreeningFiles() ScreeningFileRepository
	ContinuousConfigs() ContinuousConfigRepository
	MonitoredObjects() MonitoredObjectRepository
	DatasetUpdateJobs() DatasetUpdateJobRepository
	RawTx() pgx.Tx
}

type ScreeningRepository interface {
	Create(ctx context.Context, item screening.Screening) (screening.Screening, error)
	GetByID(ctx context.Context, tenantID, screeningID string) (screening.Screening, error)
	GetByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (screening.Screening, error)
	ListByDecision(ctx context.Context, tenantID, decisionID string) ([]screening.Screening, error)
	ListByStatus(ctx context.Context, status screening.Status, limit int) ([]screening.Screening, error)
	Update(ctx context.Context, item screening.Screening) (screening.Screening, error)
}

type ScreeningMatchRepository interface {
	ReplaceForScreening(ctx context.Context, screeningID string, items []screening.Match) error
	ListByScreening(ctx context.Context, tenantID, screeningID string) ([]screening.Match, error)
	GetByID(ctx context.Context, tenantID, matchID string) (screening.Match, error)
	Update(ctx context.Context, item screening.Match) (screening.Match, error)
	CountPendingByScreening(ctx context.Context, screeningID string) (int, error)
}

type ScreeningCommentRepository interface {
	Create(ctx context.Context, item screening.Comment) (screening.Comment, error)
	ListByMatchIDs(ctx context.Context, tenantID string, matchIDs []string) ([]screening.Comment, error)
}

type ScreeningFileRepository interface {
	Create(ctx context.Context, item screening.File) (screening.File, error)
	GetByID(ctx context.Context, tenantID, screeningID, fileID string) (screening.File, error)
	ListByScreening(ctx context.Context, tenantID, screeningID string) ([]screening.File, error)
}

type ScreeningWhitelistRepository interface {
	Create(ctx context.Context, item screening.WhitelistEntry) (screening.WhitelistEntry, error)
	Delete(ctx context.Context, tenantID, entityID string, counterpartyIdentifier *string) error
	Search(ctx context.Context, tenantID string, entityID, counterpartyIdentifier *string) ([]screening.WhitelistEntry, error)
}

type ContinuousConfigRepository interface {
	Create(ctx context.Context, item screening.ContinuousConfig) (screening.ContinuousConfig, error)
	GetByID(ctx context.Context, tenantID, configID string) (screening.ContinuousConfig, error)
	ListByTenant(ctx context.Context, tenantID string) ([]screening.ContinuousConfig, error)
	Update(ctx context.Context, item screening.ContinuousConfig) (screening.ContinuousConfig, error)
	Delete(ctx context.Context, tenantID, configID string) error
}

type MonitoredObjectRepository interface {
	Create(ctx context.Context, item screening.MonitoredObject) (screening.MonitoredObject, error)
	GetByID(ctx context.Context, tenantID, monitoredObjectID string) (screening.MonitoredObject, error)
	ListByConfig(ctx context.Context, tenantID, configID string) ([]screening.MonitoredObject, error)
	ListByStatus(ctx context.Context, status screening.MonitoredObjectStatus, limit int) ([]screening.MonitoredObject, error)
	ListByTenantAndStatus(ctx context.Context, tenantID string, status screening.MonitoredObjectStatus, limit int) ([]screening.MonitoredObject, error)
	Update(ctx context.Context, item screening.MonitoredObject) (screening.MonitoredObject, error)
	Delete(ctx context.Context, tenantID, monitoredObjectID string) error
}

type DatasetUpdateJobRepository interface {
	Create(ctx context.Context, item screening.DatasetUpdateJob) (screening.DatasetUpdateJob, error)
	GetByID(ctx context.Context, tenantID, jobID string) (screening.DatasetUpdateJob, error)
	ListByTenant(ctx context.Context, tenantID string) ([]screening.DatasetUpdateJob, error)
	ListByStatus(ctx context.Context, status screening.DatasetUpdateJobStatus, limit int) ([]screening.DatasetUpdateJob, error)
	Update(ctx context.Context, item screening.DatasetUpdateJob) (screening.DatasetUpdateJob, error)
}

type ScreeningProvider interface {
	Search(ctx context.Context, request screening.SearchRequest) (screening.ProviderResult, error)
	Enrich(ctx context.Context, provider, entityID string) (screening.EnrichmentResult, error)
	GetCatalog(ctx context.Context, provider string) (screening.DatasetCatalog, error)
	GetFreshness(ctx context.Context, provider string) (screening.DatasetFreshness, error)
	GetDatasetDelta(ctx context.Context, provider, cursor string) (screening.DatasetDelta, error)
}

type TenantRecord struct {
	ObjectID   string
	ObjectType string
	Fields     map[string]any
}

type TenantDataReader interface {
	GetRecord(ctx context.Context, tenantID, objectType, objectID string) (TenantRecord, error)
}

type Inbox struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`
}

type InboxReader interface {
	GetInbox(ctx context.Context, tenantID, inboxID string) (Inbox, error)
}

type ScreeningReviewedCommand struct {
	TenantID    string `json:"tenant_id"`
	ScreeningID string `json:"screening_id"`
	DecisionID  string `json:"decision_id,omitempty"`
	MatchID     string `json:"match_id"`
	Status      string `json:"status"`
	ReviewerID  string `json:"reviewer_id,omitempty"`
}

type ScreeningEvidenceUploadedCommand struct {
	TenantID    string `json:"tenant_id"`
	ScreeningID string `json:"screening_id"`
	FileID      string `json:"file_id"`
	UploadedBy  string `json:"uploaded_by,omitempty"`
}

type CasePublisher interface {
	PublishScreeningReviewed(ctx context.Context, command ScreeningReviewedCommand) error
	PublishScreeningEvidenceUploaded(ctx context.Context, command ScreeningEvidenceUploadedCommand) error
}

type ScreeningStatusChangedCommand struct {
	TenantID          string     `json:"tenant_id"`
	ScreeningID       string     `json:"screening_id"`
	DecisionID        string     `json:"decision_id,omitempty"`
	ScenarioID        string     `json:"scenario_id,omitempty"`
	ScreeningConfigID string     `json:"screening_config_id,omitempty"`
	Status            string     `json:"status"`
	Provider          string     `json:"provider"`
	ObjectType        string     `json:"object_type"`
	ObjectID          string     `json:"object_id"`
	ProviderReference string     `json:"provider_reference,omitempty"`
	LastError         string     `json:"last_error,omitempty"`
	Partial           bool       `json:"partial"`
	IdempotencyKey    string     `json:"idempotency_key,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	MatchCount        int        `json:"match_count"`
}

type DecisionEnginePublisher interface {
	PublishScreeningStatusChanged(ctx context.Context, command ScreeningStatusChangedCommand) error
}

type BlobUploadSession struct {
	StorageKey string `json:"storage_key"`
	UploadURL  string `json:"upload_url"`
	Method     string `json:"method"`
}

type BlobDownload struct {
	DownloadURL string `json:"download_url"`
}

type BlobStore interface {
	CreateUploadSession(ctx context.Context, tenantID, screeningID, fileName, contentType string, fileSize int64) (BlobUploadSession, error)
	GetDownloadURL(ctx context.Context, storageKey string) (BlobDownload, error)
}
