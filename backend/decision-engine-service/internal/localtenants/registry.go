// Package localtenants discovers registered tenants for the explicitly opted-in
// development host. Request paths select a scope; SQL still verifies activation.
package localtenants

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrDenied = errors.New("tenant is missing or inactive")

type Entry struct {
	ID     uuid.UUID
	Active bool
}
type Registry struct {
	DB     *sql.DB
	schema string
}

func New(db *sql.DB, database string) (*Registry, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("tenant registry pool required")
	}
	return &Registry{db, schema}, nil
}

// Scan reads a bounded primary-key page, including inactive entries so inactive
// history cannot turn an active-tenant filter into an unbounded scan.
func (r *Registry) Scan(ctx context.Context, after uuid.UUID, limit int) ([]Entry, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("tenant page must be 1..100")
	}
	rows, err := r.DB.QueryContext(ctx, "SELECT id,status='active' FROM "+r.schema+".data_model_tenants FORCE INDEX(PRIMARY) WHERE id>? ORDER BY id LIMIT ?", mysqlstore.UUID(after), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Entry, 0, limit)
	for rows.Next() {
		var id mysqlstore.UUID
		var active bool
		if err := rows.Scan(&id, &active); err != nil {
			return nil, err
		}
		out = append(out, Entry{uuid.UUID(id), active})
	}
	return out, rows.Err()
}

// Next advances even through a wholly inactive page. A zero tenant means no
// active tenant in this page, not permission to use an unscoped owner.
func (r *Registry) Next(ctx context.Context, after uuid.UUID) (tenant, next uuid.UUID, err error) {
	page, err := r.Scan(ctx, after, 20)
	if err != nil {
		return uuid.Nil, after, err
	}
	for _, e := range page {
		next = e.ID
		if e.Active {
			return e.ID, next, nil
		}
	}
	if len(page) < 20 {
		next = uuid.Nil
	}
	return uuid.Nil, next, nil
}

func (r *Registry) Authorize(ctx context.Context, tx *sql.Tx, tenant uuid.UUID) error {
	if tenant == uuid.Nil || tx == nil {
		return ErrDenied
	}
	return r.checkActive(ctx, tx, tenant)
}

// CheckActive is the request admission check: one autocommit point read needs
// no multi-statement snapshot. It grants no owner mutation capability. Owners
// must still call Authorize on their own transaction before doing any work.
func (r *Registry) CheckActive(ctx context.Context, tenant uuid.UUID) error {
	if tenant == uuid.Nil {
		return ErrDenied
	}
	return r.checkActive(ctx, r.DB, tenant)
}

type activeReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *Registry) checkActive(ctx context.Context, reader activeReader, tenant uuid.UUID) error {
	var active bool
	err := reader.QueryRowContext(ctx, "SELECT status='active' FROM "+r.schema+".data_model_tenants WHERE id=?", mysqlstore.UUID(tenant)).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !active {
		return ErrDenied
	}
	return err
}
