package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type CreateLogicalBucketRequest struct {
	TimestampFieldID uuid.UUID `json:"timestamp_field_id" binding:"required"`
	Timezone         string    `json:"timezone" binding:"required"`
}

type LogicalBucketResponse struct {
	ID                 uuid.UUID  `json:"id"`
	TenantID           uuid.UUID  `json:"tenant_id"`
	TableID            uuid.UUID  `json:"table_id"`
	TimestampFieldID   uuid.UUID  `json:"timestamp_field_id"`
	TimestampFieldName string     `json:"timestamp_field_name"`
	Grain              string     `json:"grain"`
	Timezone           string     `json:"timezone"`
	SealDelaySeconds   int64      `json:"seal_delay_seconds"`
	DefinitionVersion  int        `json:"definition_version"`
	Status             string     `json:"status"`
	IndexJobID         *uuid.UUID `json:"index_job_id,omitempty"`
	CacheEligibleAt    *time.Time `json:"cache_eligible_at,omitempty"`
	MaintenanceUntil   *time.Time `json:"maintenance_until,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	RetiredAt          *time.Time `json:"retired_at,omitempty"`
}

func AdaptLogicalBucket(item datamodel.LogicalBucketDefinition) LogicalBucketResponse {
	return LogicalBucketResponse{
		ID: item.ID, TenantID: item.TenantID, TableID: item.TableID,
		TimestampFieldID: item.TimestampFieldID, TimestampFieldName: item.TimestampFieldName,
		Grain: item.Grain, Timezone: item.Timezone,
		SealDelaySeconds:  int64(item.SealDelay / time.Second),
		DefinitionVersion: item.DefinitionVersion, Status: string(item.Status),
		IndexJobID: item.IndexJobID, CacheEligibleAt: item.CacheEligibleAt,
		MaintenanceUntil: item.MaintenanceUntil, CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt, RetiredAt: item.RetiredAt,
	}
}
