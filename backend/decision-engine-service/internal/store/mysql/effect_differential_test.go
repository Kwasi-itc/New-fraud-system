package mysql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type differentialEffectPorts struct {
	decisions ports.DecisionRepository
	workflow  ports.WorkflowExecutionRepository
	screen    ports.ScreeningExecutionRepository
	score     ports.ScoringRequestRepository
	outbox    ports.OutboxEventRepository
}

// Compare actual source/target SQL repositories, including exact transaction ID,
// large JSON numbers, nulls, owner relationships, outcomes and rollback. This
// storage corpus is NOT a claim of full browser/provider/runtime parity.
func TestEffectsPostgresMySQLDifferentialCorpus(t *testing.T) {
	ctx := context.Background()
	dsn := testsupport.PostgresDatabaseURL(t)
	dir, err := filepath.Abs("../../migrations/metadata")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.New("file://"+dir, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	if a, b := m.Close(); a != nil || b != nil {
		t.Fatal(a, b)
	}
	pg, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	f := effectsFixture(t, true)
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	screen := f.screeningInput(d)
	score := scoring.Request{ID: uuid.NewString(), TenantID: d.TenantID, ConfigID: f.score.ID, DecisionID: d.ID, ScenarioID: d.ScenarioID, Status: scoring.RequestStatusPending, RequestJSON: screen.RequestJSON, CreatedAt: d.CreatedAt, UpdatedAt: d.CreatedAt}
	wfID := f.wf.ID
	wf := workflow.Execution{ID: uuid.NewString(), TenantID: d.TenantID, WorkflowID: &wfID, DecisionID: d.ID, ScenarioID: d.ScenarioID, ActionType: f.wf.ActionType, ActionConfig: f.wf.ActionConfig, Status: workflow.ExecutionStatusPendingDispatch, CreatedAt: d.CreatedAt}
	event := jobOutboxInput(f, d)
	tm := postgres.NewTransactionManager(pg)
	if err := tm.Run(ctx, func(s ports.MutationStore) error {
		if _, err := s.Scenarios().Create(ctx, scenario.Scenario{ID: f.parent.String(), TenantID: f.tenant.String(), Name: "Effect owner", TriggerObjectType: "wallets", CreatedAt: d.CreatedAt, UpdatedAt: d.CreatedAt}); err != nil {
			return err
		}
		if _, err := s.Iterations().Create(ctx, f.iteration); err != nil {
			return err
		}
		if _, err := s.Workflows().Create(ctx, f.wf); err != nil {
			return err
		}
		if _, err := s.ScreeningConfigs().Create(ctx, f.screen); err != nil {
			return err
		}
		_, err := s.ScoringConfigs().Create(ctx, f.score)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	queued, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, f.store.screening, effectQueuePolicies())
	if err != nil {
		t.Fatal(err)
	}
	runs := []func(func(differentialEffectPorts) error) error{
		func(fn func(differentialEffectPorts) error) error {
			return tm.Run(ctx, func(s ports.MutationStore) error {
				return fn(differentialEffectPorts{s.Decisions(), s.WorkflowExecutions(), s.ScreeningExecutions(), s.ScoringRequests(), s.OutboxEvents()})
			})
		},
		func(fn func(differentialEffectPorts) error) error {
			return queued.Mutate(ctx, func(r EffectRepositories) error {
				return fn(differentialEffectPorts{r.Decisions, r.WorkflowExecutions, r.ScreeningExecutions, r.ScoringRequests, r.OutboxEvents})
			})
		},
	}
	type snapshot struct {
		Decision decision.Decision
		Workflow workflow.Execution
		Screen   screening.Execution
		Score    scoring.Request
		Event    integration.OutboxEvent
	}
	outputs := []any{}
	canonical := func(v any) any {
		t.Helper()
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var out any
		if err := decoder.Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	rollback := errors.New("fixture rollback")
	for _, run := range runs {
		persist := func(p differentialEffectPorts) error {
			if _, err := p.decisions.Create(ctx, d); err != nil {
				return err
			}
			if _, err := p.workflow.CreateMany(ctx, []workflow.Execution{wf}); err != nil {
				return err
			}
			if _, err := p.screen.CreateMany(ctx, []screening.Execution{screen}); err != nil {
				return err
			}
			if _, err := p.score.CreateMany(ctx, []scoring.Request{score}); err != nil {
				return err
			}
			_, err := p.outbox.CreateMany(ctx, []integration.OutboxEvent{event})
			return err
		}
		if err := run(func(p differentialEffectPorts) error {
			if err := persist(p); err != nil {
				return err
			}
			return rollback
		}); !errors.Is(err, rollback) {
			t.Fatal(err)
		}
		if err := run(func(p differentialEffectPorts) error {
			if _, err := p.decisions.GetByID(ctx, d.TenantID, d.ID); err == nil {
				t.Fatal("rolled back decision visible")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var out snapshot
		if err := run(func(p differentialEffectPorts) error {
			if err := persist(p); err != nil {
				return err
			}
			var err error
			out.Decision, err = p.decisions.GetByID(ctx, d.TenantID, d.ID)
			if err != nil {
				return err
			}
			out.Workflow, err = p.workflow.GetByID(ctx, d.TenantID, wf.ID)
			if err != nil {
				return err
			}
			out.Screen, err = p.screen.GetByID(ctx, d.TenantID, screen.ID)
			if err != nil {
				return err
			}
			out.Score, err = p.score.GetByID(ctx, d.TenantID, score.ID)
			if err != nil {
				return err
			}
			out.Event, err = p.outbox.GetByID(ctx, d.TenantID, event.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, canonical(out))
	}
	if !reflect.DeepEqual(outputs[0], outputs[1]) {
		t.Fatalf("effect storage differs\nPG=%#v\nMySQL=%#v", outputs[0], outputs[1])
	}
}
