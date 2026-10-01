package ports

import (
	"context"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

type ReportRepository interface {
	Create(context.Context, casepkg.Report) error
	Get(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, bool) (casepkg.Report, error)
	List(context.Context, uuid.UUID, uuid.UUID, *casepkg.Cursor, int) (casepkg.ReportPage, error)
	Save(context.Context, casepkg.Report, int) error
	Evidence(context.Context, uuid.UUID, uuid.UUID, []uuid.UUID) ([]casepkg.EvidenceReference, error)
}
