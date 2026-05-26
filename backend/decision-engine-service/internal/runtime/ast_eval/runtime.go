package ast_eval

import (
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type Runtime struct {
	TenantID         string
	ObjectID         string
	ObjectType       string
	Fields           map[string]any
	Now              time.Time
	Model            *ports.TenantModel
	TenantDataReader ports.TenantDataReader
	CustomListRepo   ports.CustomListRepository
	RecordTagRepo    ports.RecordTagRepository
	RiskRepo         ports.RiskSnapshotRepository
	IPFlagRepo       ports.IPFlagRepository
	DecisionRepo     ports.DecisionRepository
}

type ScoreComputationResult struct {
	Triggered bool `json:"triggered"`
	Modifier  int  `json:"modifier"`
	Floor     int  `json:"floor"`

	Branch   *int `json:"branch,omitempty"`
	Fallback bool `json:"fallback"`
	Default  bool `json:"default"`
}
