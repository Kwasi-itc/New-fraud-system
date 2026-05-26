package ast_eval

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type Runtime struct {
	TenantID         string
	ObjectID         string
	ObjectType       string
	Fields           map[string]any
	Model            *ports.TenantModel
	TenantDataReader ports.TenantDataReader
	CustomListRepo   ports.CustomListRepository
	RecordTagRepo    ports.RecordTagRepository
	RiskRepo         ports.RiskSnapshotRepository
	IPFlagRepo       ports.IPFlagRepository
	DecisionRepo     ports.DecisionRepository
}
