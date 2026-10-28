package installation

import (
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
)

// ExtendPlan preserves a reviewed installed artifact byte-for-byte and appends
// reviewed upgrade steps. It does not regenerate old steps from current source,
// inspect/adopt target drift or rewrite checksums. The caller supplies canonical
// before/after observations from an isolated upgrade reference installation.
func ExtendPlan(installed migrations.Plan, upgrade []migrations.Step) (migrations.Plan, error) {
	if err := installed.Validate(); err != nil {
		return migrations.Plan{}, err
	}
	if len(upgrade) == 0 || len(installed.Steps)+len(upgrade) > 10000 {
		return migrations.Plan{}, errors.New("nonempty bounded upgrade required")
	}
	known := map[string]bool{}
	for _, s := range installed.Steps {
		known[s.Kind+":"+s.Name] = true
	}
	for _, s := range upgrade {
		key := s.Kind + ":" + s.Name
		if !known[key] && s.Before != "" {
			return migrations.Plan{}, errors.New("upgrade cannot adopt unjournaled objects")
		}
		known[key] = true
	}
	steps := make([]migrations.Step, 0, len(installed.Steps)+len(upgrade))
	steps = append(steps, installed.Steps...)
	steps = append(steps, upgrade...)
	plan := migrations.Plan{Database: installed.Database, Steps: steps}
	if err := plan.Validate(); err != nil {
		return migrations.Plan{}, err
	}
	return plan, nil
}
