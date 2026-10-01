package service

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
	"time"
)

func (s CaseService) CaseAnalytics(ctx context.Context, tenant uuid.UUID, f casepkg.AnalyticsFilter) (casepkg.Analytics, error) {
	if err := requireIDs(tenant); err != nil {
		return casepkg.Analytics{}, err
	}
	if f.From.IsZero() || f.To.IsZero() || !f.From.Before(f.To) || f.To.Sub(f.From) > 366*24*time.Hour {
		return casepkg.Analytics{}, casepkg.Invalid("from and to must define a positive interval of at most 366 days")
	}
	if f.InboxID != nil {
		if err := requireIDs(*f.InboxID); err != nil {
			return casepkg.Analytics{}, err
		}
	}
	p, err := access.Tenant(ctx, tenant)
	if err != nil {
		return casepkg.Analytics{}, err
	}
	f.AccessUserID = ""
	if p.Kind == access.User && !p.Admin {
		f.AccessUserID = p.Subject
	}
	f.From = f.From.UTC()
	f.To = f.To.UTC()
	return s.analytics.Analytics(ctx, tenant, f)
}
