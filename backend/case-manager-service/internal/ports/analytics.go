package ports

import (
	"context"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

type AnalyticsRepository interface {
	Analytics(context.Context, uuid.UUID, casepkg.AnalyticsFilter) (casepkg.Analytics, error)
}
