package installation

import (
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// DeliveryRetryUpgradeStatement is appended to an existing reviewed lineage.
// NULL deliberately means unknown history for pre-upgrade deliveries; claims
// cannot reconstruct the number of execution errors. New policy-aware enqueue
// writes zero explicitly. This does not change the frozen v001 definitions.
func DeliveryRetryUpgradeStatement(database string) (migrations.Step, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return migrations.Step{}, err
	}
	return migrations.Step{ID: "data_model_v002_delivery_error_count", Kind: "TABLE", Name: "data_model_index_deliveries", SQL: "ALTER TABLE " + schema + ".data_model_index_deliveries ADD COLUMN error_count INT NULL DEFAULT NULL, ADD CONSTRAINT data_model_delivery_error_count CHECK (error_count>=0 AND error_count<=attempt)"}, nil
}

// ExtendDeliveryRetryPlan binds trusted isolated before/after observations to
// the shipped upgrade SQL, preserving every installed step/checksum unchanged.
func ExtendDeliveryRetryPlan(installed migrations.Plan, reference migrations.Step) (migrations.Plan, error) {
	step, err := DeliveryRetryUpgradeStatement(installed.Database)
	if err != nil {
		return migrations.Plan{}, err
	}
	if step.ID != reference.ID || step.Kind != reference.Kind || step.Name != reference.Name || step.SQL != reference.SQL || reference.Before == "" {
		return migrations.Plan{}, errors.New("delivery retry reference differs from reviewed upgrade")
	}
	step.Before, step.After = reference.Before, reference.After
	return ExtendPlan(installed, []migrations.Step{step})
}
