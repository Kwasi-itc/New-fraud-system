package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/google/uuid"
	"net/url"
	"strconv"
	"time"
)

type historyCursor struct {
	Time time.Time `json:"time"`
	ID   string    `json:"id"`
}

func ParseManualHistoryFilter(q url.Values) (screening.ManualHistoryFilter, error) {
	f := screening.ManualHistoryFilter{Limit: 20, Status: q.Get("status"), EntityType: q.Get("entity_type")}
	for key, values := range q {
		if len(values) != 1 {
			return f, ErrManualInput
		}
		switch key {
		case "limit", "status", "entity_type", "from", "to", "cursor":
		default:
			return f, ErrManualInput
		}
	}
	if q.Get("limit") != "" {
		n, e := strconv.Atoi(q.Get("limit"))
		if e != nil || n < 1 || n > 50 {
			return f, ErrManualInput
		}
		f.Limit = n
	}
	switch f.Status {
	case "", "pending", "queued", "processing", "awaiting_review", "no_hit", "confirmed_hit", "failed", "archived":
	default:
		return f, ErrManualInput
	}
	if f.EntityType != "" && f.EntityType != "Person" && f.EntityType != "Organization" {
		return f, ErrManualInput
	}
	if v := q.Get("from"); v != "" {
		d, e := time.Parse("2006-01-02", v)
		if e != nil {
			return f, ErrManualInput
		}
		f.From = &d
	}
	if v := q.Get("to"); v != "" {
		d, e := time.Parse("2006-01-02", v)
		if e != nil {
			return f, ErrManualInput
		}
		d = d.AddDate(0, 0, 1)
		f.Until = &d
	}
	if f.From != nil && f.Until != nil && !f.From.Before(*f.Until) {
		return f, ErrManualInput
	}
	if v := q.Get("cursor"); v != "" {
		if len(v) > 512 {
			return f, ErrManualInput
		}
		raw, e := base64.RawURLEncoding.DecodeString(v)
		var c historyCursor
		if e != nil || json.Unmarshal(raw, &c) != nil || c.Time.IsZero() {
			return f, ErrManualInput
		}
		id, e := uuid.Parse(c.ID)
		if e != nil || id == uuid.Nil {
			return f, ErrManualInput
		}
		f.Before = &c.Time
		f.BeforeID = id.String()
	}
	return f, nil
}
func (s ScreeningService) ListManualSearches(ctx context.Context, tenant, actor string, f screening.ManualHistoryFilter) (screening.ManualHistoryPage, error) {
	page := screening.ManualHistoryPage{Items: []screening.ManualHistoryItem{}}
	if f.Limit < 1 || f.Limit > 50 {
		return page, ErrManualInput
	}
	items, err := s.screeningRepo.ListManualHistory(ctx, tenant, actor, f)
	if err != nil {
		return page, err
	}
	if len(items) > f.Limit {
		items = items[:f.Limit]
		last := items[len(items)-1]
		raw, _ := json.Marshal(historyCursor{last.CreatedAt, last.ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	if items != nil {
		page.Items = items
	}
	return page, nil
}
