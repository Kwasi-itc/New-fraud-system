package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
	"strings"
)

func validateReport(v casepkg.ReportContent, complete bool) error {
	if strings.TrimSpace(v.Title) == "" || len(v.Title) > 200 || len(v.Subject) > 1000 || len(v.Narrative) > 50000 || len(v.FileIDs) > 50 {
		return casepkg.Invalid("report requires a title (200 characters maximum), subject up to 1000, narrative up to 50000, and at most 50 attachments")
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range v.FileIDs {
		if id == uuid.Nil || seen[id] {
			return casepkg.Invalid("invalid or duplicate attachment")
		}
		seen[id] = true
	}
	if v.ActivityFrom != nil && v.ActivityTo != nil && v.ActivityTo.Before(*v.ActivityFrom) {
		return casepkg.Invalid("activity end must not precede start")
	}
	if complete && (strings.TrimSpace(v.Subject) == "" || strings.TrimSpace(v.Narrative) == "" || v.ActivityFrom == nil || v.ActivityTo == nil) {
		return casepkg.Invalid("completion requires subject, narrative, and activity start/end")
	}
	return nil
}

func (s CaseService) ListReports(ctx context.Context, tenant, id uuid.UUID, before *casepkg.Cursor, limit int) (casepkg.ReportPage, error) {
	if err := validLimit(limit); err != nil {
		return casepkg.ReportPage{}, err
	}
	return transact(ctx, s, func(tx CaseService) (casepkg.ReportPage, error) {
		if _, err := tx.workspaceCase(ctx, tenant, id); err != nil {
			return casepkg.ReportPage{}, err
		}
		return tx.reports.List(ctx, tenant, id, before, limit)
	})
}
func (s CaseService) GetReport(ctx context.Context, tenant, id, report uuid.UUID) (casepkg.Report, error) {
	return transact(ctx, s, func(tx CaseService) (casepkg.Report, error) {
		if _, err := tx.workspaceCase(ctx, tenant, id); err != nil {
			return casepkg.Report{}, err
		}
		return tx.reports.Get(ctx, tenant, id, report, false)
	})
}
func (s CaseService) CreateReport(ctx context.Context, tenant, id, report uuid.UUID, content casepkg.ReportContent) (casepkg.Report, error) {
	if err := requireIDs(tenant, id, report); err != nil {
		return casepkg.Report{}, err
	}
	if err := validateReport(content, false); err != nil {
		return casepkg.Report{}, err
	}
	actor := access.Actor(ctx)
	if actor == nil {
		return casepkg.Report{}, casepkg.ErrForbidden
	}
	return transact(ctx, s, func(tx CaseService) (casepkg.Report, error) {
		if _, err := tx.caseForMutation(ctx, tenant, id, actor); err != nil {
			return casepkg.Report{}, err
		}
		existing, err := tx.reports.Get(ctx, tenant, id, report, true)
		if err == nil {
			a, e := json.Marshal(content)
			if e != nil {
				return existing, e
			}
			b, e := json.Marshal(existing.Payload.Content)
			if e != nil {
				return existing, e
			}
			if existing.CreatedBy == *actor && existing.Version == 1 && string(a) == string(b) {
				return existing, nil
			}
			return existing, casepkg.ErrConflict
		}
		if !errors.Is(err, casepkg.ErrNotFound) {
			return casepkg.Report{}, err
		}
		if _, err := tx.reports.Evidence(ctx, tenant, id, content.FileIDs); err != nil {
			return casepkg.Report{}, err
		}
		now := tx.clock.Now()
		v := casepkg.Report{ID: report, TenantID: tenant, CaseID: id, Status: "draft", Version: 1, Payload: casepkg.ReportPayload{Format: "internal_sar_v1", Content: content}, CreatedBy: *actor, CreatedAt: now, UpdatedAt: now}
		if err := tx.reports.Create(ctx, v); err != nil {
			return v, err
		}
		return v, tx.reportEvent(ctx, v, actor, "report_created", nil)
	})
}
func (s CaseService) ChangeReport(ctx context.Context, tenant, id, report uuid.UUID, version int, content *casepkg.ReportContent, complete bool) (casepkg.Report, error) {
	if complete && content != nil {
		return casepkg.Report{}, casepkg.Invalid("save the draft before completing it")
	}
	if version < 1 {
		return casepkg.Report{}, casepkg.Invalid("expected version is required")
	}
	actor := access.Actor(ctx)
	if actor == nil {
		return casepkg.Report{}, casepkg.ErrForbidden
	}
	if !complete && content == nil {
		return casepkg.Report{}, casepkg.Invalid("report content is required")
	}
	return transact(ctx, s, func(tx CaseService) (casepkg.Report, error) {
		item, err := tx.caseForMutation(ctx, tenant, id, actor)
		if err != nil {
			return casepkg.Report{}, err
		}
		v, err := tx.reports.Get(ctx, tenant, id, report, true)
		if err != nil {
			return v, err
		}
		if complete && v.Status == "completed" && v.Version == version+1 && v.CompletedBy != nil && *v.CompletedBy == *actor {
			return v, nil
		}
		if v.Status != "draft" || v.Version != version || v.Payload.Format != "internal_sar_v1" {
			return v, casepkg.ErrConflict
		}
		previous := v
		if content != nil {
			v.Payload.Content = *content
		}
		if err := validateReport(v.Payload.Content, complete); err != nil {
			return v, err
		}
		refs, err := tx.reports.Evidence(ctx, tenant, id, v.Payload.Content.FileIDs)
		if err != nil {
			return v, err
		}
		kind := "report_updated"
		v.Version++
		v.UpdatedAt = tx.clock.Now()
		if complete {
			kind = "report_completed"
			v.Status = "completed"
			v.CompletedBy = actor
			v.CompletedAt = &v.UpdatedAt
			v.Payload.CaseSnapshot = &item
			v.Payload.Evidence = refs
		}
		if err := tx.reports.Save(ctx, v, version); err != nil {
			return v, err
		}
		return v, tx.reportEvent(ctx, v, actor, kind, &previous)
	})
}
func (s CaseService) reportEvent(ctx context.Context, v casepkg.Report, actor *string, kind string, previous *casepkg.Report) error {
	next, err := json.Marshal(v)
	if err != nil {
		return err
	}
	old := ""
	if previous != nil {
		b, err := json.Marshal(previous)
		if err != nil {
			return err
		}
		old = string(b)
	}
	if err := s.contributors.Add(ctx, v.TenantID, v.CaseID, *actor, s.clock.Now()); err != nil {
		return err
	}
	_, err = s.events.Create(ctx, newEvent(s.ids.New(), v.TenantID, v.CaseID, actor, kind, "", v.ID.String(), "suspicious_activity_report", string(next), old, s.clock.Now()))
	return err
}
