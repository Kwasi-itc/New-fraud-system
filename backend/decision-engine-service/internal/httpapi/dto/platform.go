package dto

import (
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
)

type CreateCustomListEntryRequest struct {
	ListName string `json:"list_name"`
	Value    string `json:"value"`
}

type CreateRecordTagRequest struct {
	ObjectType string `json:"object_type"`
	ObjectID   string `json:"object_id"`
	Tag        string `json:"tag"`
}

type CreateRiskSnapshotRequest struct {
	ObjectType string `json:"object_type"`
	ObjectID   string `json:"object_id"`
	RiskLevel  string `json:"risk_level"`
}

type CreateIPFlagRequest struct {
	IPAddress string `json:"ip_address"`
	Flag      string `json:"flag"`
}

type CustomListEntryResponse struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	ListName  string    `json:"list_name"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at"`
}

type RecordTagResponse struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	ObjectType string    `json:"object_type"`
	ObjectID   string    `json:"object_id"`
	Tag        string    `json:"tag"`
	CreatedAt  time.Time `json:"created_at"`
}

type RiskSnapshotResponse struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	ObjectType string    `json:"object_type"`
	ObjectID   string    `json:"object_id"`
	RiskLevel  string    `json:"risk_level"`
	CreatedAt  time.Time `json:"created_at"`
}

type IPFlagResponse struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	IPAddress string    `json:"ip_address"`
	Flag      string    `json:"flag"`
	CreatedAt time.Time `json:"created_at"`
}

func AdaptCustomListEntry(item platform.CustomListEntry) CustomListEntryResponse {
	return CustomListEntryResponse{ID: item.ID, TenantID: item.TenantID, ListName: item.ListName, Value: item.Value, CreatedAt: item.CreatedAt}
}

func AdaptRecordTag(item platform.RecordTag) RecordTagResponse {
	return RecordTagResponse{ID: item.ID, TenantID: item.TenantID, ObjectType: item.ObjectType, ObjectID: item.ObjectID, Tag: item.Tag, CreatedAt: item.CreatedAt}
}

func AdaptRiskSnapshot(item platform.RiskSnapshot) RiskSnapshotResponse {
	return RiskSnapshotResponse{ID: item.ID, TenantID: item.TenantID, ObjectType: item.ObjectType, ObjectID: item.ObjectID, RiskLevel: item.RiskLevel, CreatedAt: item.CreatedAt}
}

func AdaptIPFlag(item platform.IPFlag) IPFlagResponse {
	return IPFlagResponse{ID: item.ID, TenantID: item.TenantID, IPAddress: item.IPAddress, Flag: item.Flag, CreatedAt: item.CreatedAt}
}
