package mysqlexecution

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

type scoringConfigResult struct {
	items []scoring.Config
	err   error
}

func (r scoringConfigResult) ListActiveByScenario(context.Context, string, string) ([]scoring.Config, error) {
	return r.items, r.err
}

func TestDeferredScoringRejectsConfigurationAndPropagatesFailures(t *testing.T) {
	failure := errors.New("owner unavailable")
	for _, tc := range []struct {
		name  string
		input scoringConfigResult
		want  error
	}{
		{"empty", scoringConfigResult{}, nil},
		{"active", scoringConfigResult{items: []scoring.Config{{}}}, mysql.ErrAdditionalScoringDeferred},
		{"read failure", scoringConfigResult{err: failure}, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, err := (deferredScoringConfigs{tc.input}).ListActiveByScenario(context.Background(), "tenant", "scenario")
			if !errors.Is(err, tc.want) || len(items) != 0 {
				t.Fatal(items, err)
			}
		})
	}
}
