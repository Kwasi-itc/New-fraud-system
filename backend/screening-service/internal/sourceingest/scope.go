package sourceingest

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s Store) checkKey(key Key) error {
	if s.GlobalOnly && key.Owner != uuid.Nil {
		return sourcedata.ErrPublish
	}
	if s.Scope != nil && (*s.Scope == uuid.Nil || s.Guard == nil || key.Owner != *s.Scope) {
		return sourcedata.ErrPublish
	}
	return nil
}
func (s Store) begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if s.Guard != nil {
		if err = s.Guard(ctx, tx); err != nil {
			tx.Rollback(ctx)
			return nil, err
		}
	}
	return tx, nil
}
