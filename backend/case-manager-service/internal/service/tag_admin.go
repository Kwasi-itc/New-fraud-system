package service

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
	"strings"
)

func (s CaseService) UpdateTag(ctx context.Context, tenant, id uuid.UUID, name, color string) (casepkg.Tag, error) {
	if err := access.Administrator(ctx, tenant); err != nil {
		return casepkg.Tag{}, err
	}
	if err := requireIDs(tenant, id); err != nil {
		return casepkg.Tag{}, err
	}
	name = strings.TrimSpace(name)
	color = strings.TrimSpace(color)
	if name == "" || len(name) > 200 || len(color) > 64 {
		return casepkg.Tag{}, casepkg.Invalid("tag name must be 1..200 bytes and color at most 64 bytes")
	}
	return transact(ctx, s, func(tx CaseService) (casepkg.Tag, error) {
		tag, err := tx.tags.Lock(ctx, tenant, id)
		if err != nil {
			return tag, err
		}
		if tag.DeletedAt != nil {
			return tag, casepkg.ErrConflict
		}
		tag.Name = name
		tag.Color = color
		tag.UpdatedAt = tx.clock.Now()
		return tx.tags.Update(ctx, tag)
	})
}

func (s CaseService) ArchiveTag(ctx context.Context, tenant, id uuid.UUID) error {
	if err := access.Administrator(ctx, tenant); err != nil {
		return err
	}
	if err := requireIDs(tenant, id); err != nil {
		return err
	}
	_, err := transact(ctx, s, func(tx CaseService) (struct{}, error) {
		tag, err := tx.tags.Lock(ctx, tenant, id)
		if err != nil {
			return struct{}{}, err
		}
		if tag.DeletedAt != nil {
			return struct{}{}, nil
		}
		return struct{}{}, tx.tags.SoftDelete(ctx, tenant, id, tx.clock.Now())
	})
	return err
}
