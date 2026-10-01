package ports

import (
	"context"
	"time"

	"github.com/google/uuid"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
)

type IDGenerator interface {
	New() uuid.UUID
}

type Clock interface {
	Now() time.Time
}

// Repositories is one consistent repository scope: either a pool or one transaction.
type Repositories struct {
	Analytics    AnalyticsRepository
	Reports      ReportRepository
	Uploads      UploadRepository
	Workspace    WorkspaceRepository
	Intake       IntakeRepository
	Inboxes      InboxRepository
	Cases        CaseRepository
	Decisions    DecisionLinkRepository
	Screenings   ScreeningLinkRepository
	Tags         TagRepository
	Events       EventRepository
	Files        FileRepository
	Memberships  MembershipRepository
	Contributors ContributorRepository
	References   ReferenceRepository
}

type UploadRepository interface {
	Create(context.Context, casepkg.Upload) error
	Lock(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (casepkg.Upload, error)
	Save(context.Context, casepkg.Upload) error
}

type WorkspaceRepository interface {
	HasLink(context.Context, uuid.UUID, uuid.UUID, string, uuid.UUID) (bool, error)
	Queue(context.Context, uuid.UUID, casepkg.WorkspaceFilters, int) (casepkg.QueuePage, error)
	Links(context.Context, uuid.UUID, uuid.UUID, string, *casepkg.Cursor, int) (casepkg.LinkPage, error)
}

type IntakeReceipt struct {
	TenantID    uuid.UUID
	Source      string
	EventID     uuid.UUID
	PayloadHash string
	CaseID      uuid.UUID
	CreatedAt   time.Time
}

type IntakeRepository interface {
	Lock(context.Context, uuid.UUID, string, string) error
	Get(context.Context, uuid.UUID, string, uuid.UUID) (*IntakeReceipt, error)
	Create(context.Context, IntakeReceipt) error
}

type UnitOfWork interface {
	WithinTransaction(context.Context, func(Repositories) error) error
}

type InboxRepository interface {
	// LockShared permits concurrent validations while blocking archive/config edits.
	LockShared(ctx context.Context, tenantID, inboxID uuid.UUID) (casepkg.Inbox, error)
	LockUpdate(ctx context.Context, tenantID, inboxID uuid.UUID) (casepkg.Inbox, error)
	Create(ctx context.Context, inbox casepkg.Inbox) (casepkg.Inbox, error)
	Get(ctx context.Context, tenantID, inboxID uuid.UUID) (casepkg.Inbox, error)
	List(ctx context.Context, tenantID uuid.UUID, accessUserID string) ([]casepkg.Inbox, error)
	Update(ctx context.Context, inbox casepkg.Inbox) (casepkg.Inbox, error)
}

type CaseRepository interface {
	LockShared(ctx context.Context, tenantID, caseID uuid.UUID) (casepkg.Case, error)
	Lock(ctx context.Context, tenantID, caseID uuid.UUID) (casepkg.Case, error)
	Create(ctx context.Context, item casepkg.Case) (casepkg.Case, error)
	Get(ctx context.Context, tenantID, caseID uuid.UUID) (casepkg.Case, error)
	List(ctx context.Context, tenantID uuid.UUID, filters casepkg.CaseFilters, limit int) ([]casepkg.Case, error)
	Update(ctx context.Context, item casepkg.Case) (casepkg.Case, error)
	Assign(ctx context.Context, tenantID, caseID uuid.UUID, assignee *string, updatedAt time.Time) error
	Snooze(ctx context.Context, tenantID, caseID uuid.UUID, until *time.Time, updatedAt time.Time) error
}

type DecisionLinkRepository interface {
	FindCase(ctx context.Context, tenantID, decisionID uuid.UUID) (*casepkg.Case, error)
	Create(ctx context.Context, item casepkg.DecisionLink) (casepkg.DecisionLink, error)
	ListByCase(ctx context.Context, tenantID, caseID uuid.UUID) ([]casepkg.DecisionLink, error)
	FindOpenByPivot(ctx context.Context, tenantID uuid.UUID, inboxID *uuid.UUID, pivotValue string) (*casepkg.Case, error)
}

type ScreeningLinkRepository interface {
	FindCase(ctx context.Context, tenantID, screeningID uuid.UUID) (*casepkg.Case, error)
	Create(ctx context.Context, item casepkg.ScreeningLink) (casepkg.ScreeningLink, error)
	ListByCase(ctx context.Context, tenantID, caseID uuid.UUID) ([]casepkg.ScreeningLink, error)
}

type TagRepository interface {
	Lock(context.Context, uuid.UUID, uuid.UUID) (casepkg.Tag, error)
	ListByCases(ctx context.Context, tenantID uuid.UUID, caseIDs []uuid.UUID) (map[uuid.UUID][]casepkg.Tag, error)
	Create(ctx context.Context, tag casepkg.Tag) (casepkg.Tag, error)
	Get(ctx context.Context, tenantID, tagID uuid.UUID) (casepkg.Tag, error)
	List(ctx context.Context, tenantID uuid.UUID, target string) ([]casepkg.Tag, error)
	Update(ctx context.Context, tag casepkg.Tag) (casepkg.Tag, error)
	SoftDelete(ctx context.Context, tenantID, tagID uuid.UUID, deletedAt time.Time) error
	AddToCase(ctx context.Context, tenantID, caseID, tagID, id uuid.UUID, createdAt time.Time) error
	RemoveFromCase(ctx context.Context, tenantID, caseID, tagID uuid.UUID, deletedAt time.Time) error
	ListByCase(ctx context.Context, tenantID, caseID uuid.UUID) ([]casepkg.Tag, error)
}

type EventRepository interface {
	Create(ctx context.Context, event casepkg.Event) (casepkg.Event, error)
	ListByCase(ctx context.Context, tenantID, caseID uuid.UUID, limit int) ([]casepkg.Event, error)
}

type FileRepository interface {
	Create(ctx context.Context, file casepkg.File) (casepkg.File, error)
	ListByCase(ctx context.Context, tenantID, caseID uuid.UUID) ([]casepkg.File, error)
}

type MembershipRepository interface {
	RecordChange(ctx context.Context, tenantID, inboxID uuid.UUID, actor *string, kind, userID string, autoAssign bool, at time.Time, capacity int) error
	Has(ctx context.Context, tenantID, inboxID uuid.UUID, userID string) (bool, error)
	List(ctx context.Context, tenantID, inboxID uuid.UUID) ([]casepkg.InboxUser, error)
	Put(ctx context.Context, member casepkg.InboxUser) (casepkg.InboxUser, error)
	Remove(ctx context.Context, tenantID, inboxID uuid.UUID, userID string) error
}

type ContributorRepository interface {
	Add(ctx context.Context, tenantID, caseID uuid.UUID, userID string, at time.Time) error
	List(ctx context.Context, tenantID, caseID uuid.UUID) ([]casepkg.Contributor, error)
}

// ReferenceRepository is a read-only contract over service-owned shared schemas.
type ReferenceRepository interface {
	Decisions(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]casepkg.DecisionReference, error)
	Screening(ctx context.Context, tenantID, screeningID uuid.UUID, matchID string) (casepkg.ScreeningReference, error)
	ScreeningFile(ctx context.Context, tenantID, caseID, fileID uuid.UUID) (casepkg.File, error)
}
