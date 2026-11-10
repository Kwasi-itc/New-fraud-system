package mysql

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

func (s *Store) GetInbox(ctx context.Context, t, inbox string) (ports.Inbox, error) {
	var v ports.Inbox
	var active bool
	e := s.q.QueryRowContext(ctx, "SELECT id,tenant_id,active FROM "+s.schema+".case_manager_inboxes WHERE tenant_id=? AND id=?", id(t), id(inbox)).Scan(uid(&v.ID), uid(&v.TenantID), &active)
	if e != nil {
		return v, normalize(e)
	}
	v.Status = "archived"
	if active {
		v.Status = "active"
	}
	return v, nil
}
func (s *Store) ResetDataset(ctx context.Context, t, key string) error {
	_, e := s.q.ExecContext(ctx, "UPDATE "+s.table("dataset_jobs")+" SET status='pending',updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND status='processing'", id(t), id(key))
	return normalize(e)
}
func (s *Store) guardInbox(ctx context.Context, t string, inbox *string) error {
	if inbox == nil {
		return nil
	}
	var active bool
	e := s.q.QueryRowContext(ctx, "SELECT active FROM "+s.schema+".case_manager_inboxes WHERE tenant_id=? AND id=? FOR SHARE", id(t), id(*inbox)).Scan(&active)
	if e != nil {
		return normalize(e)
	}
	if !active {
		return errors.New("active review inbox required")
	}
	return nil
}
