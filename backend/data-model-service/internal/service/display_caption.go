package service

import (
	"context"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

// UpdateDisplayCaption preserves the ordinary caption/index contract inside the
// caller's atomic display-settings transaction.
func (s TableService) UpdateDisplayCaption(ctx context.Context, store ports.MutationStore, id uuid.UUID, caption string) error {
	table, err := store.Tables().GetByID(ctx, id)
	if err != nil {
		return err
	}
	if caption != "" {
		fields, err := store.Fields().ListByTable(ctx, id)
		if err != nil {
			return err
		}
		field, ok := findTableFieldByName(fields, caption)
		if !ok || field.Archived || field.DataType != datamodel.DataTypeString {
			return fmt.Errorf("caption must be an active string field")
		}
	}
	table.CaptionField = caption
	table.UpdatedAt = s.clock.Now()
	if err = store.Tables().Update(ctx, table); err != nil {
		return err
	}
	if caption != "" {
		_, _, err = ensureManagedIndexJobTx(ctx, store, s.indexJobEnqueuer, s.idGenerator, table.TenantID, table, datamodel.IndexJobTypeSearch, []string{caption}, "update_table_caption_field", table.UpdatedAt)
	}
	return err
}
