package mysqlexecution

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

type localWorkflowResult struct {
	active bool
	err    error
}

func (r localWorkflowResult) ListActiveByScenario(context.Context, string, string) ([]workflow.Definition, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.active {
		return []workflow.Definition{{}}, nil
	}
	return nil, nil
}

type localScreeningResult struct {
	active bool
	err    error
}

func (r localScreeningResult) ListActiveByScenario(context.Context, string, string) ([]screening.Config, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.active {
		return []screening.Config{{}}, nil
	}
	return nil, nil
}

func TestLocalCoreEffectsRejectActiveConfigurationAndStorageFailures(t *testing.T) {
	failure := errors.New("storage unavailable")
	for _, tc := range []struct {
		name      string
		active    bool
		err, want error
	}{{"empty", false, nil, nil}, {"configured", true, nil, mysql.ErrOwnerContractUnavailable}, {"storage failure", false, failure, failure}} {
		t.Run(tc.name, func(t *testing.T) {
			work, err := (deferredWorkflows{localWorkflowResult{tc.active, tc.err}}).ListActiveByScenario(context.Background(), "tenant", "scenario")
			if len(work) != 0 || !errors.Is(err, tc.want) {
				t.Fatal(work, err)
			}
			screen, err := (deferredScreening{localScreeningResult{tc.active, tc.err}}).ListActiveByScenario(context.Background(), "tenant", "scenario")
			if len(screen) != 0 || !errors.Is(err, tc.want) {
				t.Fatal(screen, err)
			}
		})
	}
}
