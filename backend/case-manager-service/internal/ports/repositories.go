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

type InboxRepository interface {
	Create(ctx context.Context, inbox casepkg.Inbox) (casepkg.Inbox, error)
	Get(ctx context.Context, tenantID, inboxID uuid.UUID) (casepkg.Inbox, error)
	List(ctx context.Context, tenantID uuid.UUID) ([]casepkg.Inbox, error)
	Update(ctx context.Context, inbox casepkg.Inbox) (casepkg.Inbox, error)
}

type CaseRepository interface {
	Create(ctx context.Context, item casepkg.Case) (casepkg.Case, error)
	Get(ctx context.Context, tenantID, caseID uuid.UUID) (casepkg.Case, error)
	List(ctx context.Context, tenantID uuid.UUID, filters casepkg.CaseFilters, limit int) ([]casepkg.Case, error)
	Update(ctx context.Context, item casepkg.Case) (casepkg.Case, error)
	Assign(ctx context.Context, tenantID, caseID uuid.UUID, assignee *string, updatedAt time.Time) error
	Snooze(ctx context.Context, tenantID, caseID uuid.UUID, until *time.Time, updatedAt time.Time) error
}

type DecisionLinkRepository interface {
	Create(ctx context.Context, item casepkg.DecisionLink) (casepkg.DecisionLink, error)
	ListByCase(ctx context.Context, tenantID, caseID uuid.UUID) ([]casepkg.DecisionLink, error)
	FindOpenByPivot(ctx context.Context, tenantID uuid.UUID, inboxID *uuid.UUID, pivotValue string) (*casepkg.Case, error)
}

type ScreeningLinkRepository interface {
	Create(ctx context.Context, item casepkg.ScreeningLink) (casepkg.ScreeningLink, error)
	ListByCase(ctx context.Context, tenantID, caseID uuid.UUID) ([]casepkg.ScreeningLink, error)
}

type TagRepository interface {
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
