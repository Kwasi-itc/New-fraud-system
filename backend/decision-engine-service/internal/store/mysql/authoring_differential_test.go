package mysql

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type authoringPorts struct {
	scenarios  ports.ScenarioRepository
	iterations ports.ScenarioIterationRepository
	rules      ports.RuleRepository
}

func TestAuthoringPostgresMySQLDifferentialCorpus(t *testing.T) {
	dsn := testsupport.PostgresDatabaseURL(t)
	directory, err := filepath.Abs("../../migrations/metadata")
	if err != nil {
		t.Fatal(err)
	}
	migrator, err := migrate.New("file://"+directory, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrator.Up(); err != nil {
		t.Fatal(err)
	}
	if src, database := migrator.Close(); src != nil || database != nil {
		t.Fatal(src, database)
	}
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	f := scenarioFoundationDB(t, true)
	tenant, parent, iteration := uuid.New(), uuid.New(), uuid.New()
	store := authoringStore(t, f, tenant, 100, 1<<20)
	pgRun := func(fn func(authoringPorts) error) error {
		return postgres.NewTransactionManager(pg).Run(ctx, func(s ports.MutationStore) error { return fn(authoringPorts{s.Scenarios(), s.Iterations(), s.Rules()}) })
	}
	mysqlRun := func(fn func(authoringPorts) error) error {
		return store.Mutate(ctx, func(s AuthoringRepositories) error { return fn(authoringPorts{s.Scenarios, s.Iterations, s.Rules}) })
	}
	at := time.Date(2026, 10, 29, 12, 0, 0, 123456000, time.UTC)
	input := scenario.Scenario{ID: parent.String(), TenantID: tenant.String(), Name: "Exact wallet rule scenario", Description: "source/target corpus", TriggerObjectType: "wallets", CreatedAt: at, UpdatedAt: at}
	draft := authoringDraft(tenant, parent)
	draft.ID = iteration.String()
	draft.CreatedAt = at
	rules := []scenario.Rule{authoringRule(tenant, iteration.String(), 1), authoringRule(tenant, iteration.String(), 2)}
	for i := range rules {
		rules[i].CreatedAt = at
		rules[i].UpdatedAt = at
	}
	rules[1].Formula = json.RawMessage(`{"function":"gte","children":[{"constant":100},{"constant":1}]}`)
	type snapshot struct {
		Scenario   scenario.Scenario
		Iterations []scenario.Iteration
		Rules      []scenario.Rule
		Groups     []string
	}
	outputs := make([]snapshot, 0, 2)
	for _, run := range []func(func(authoringPorts) error) error{pgRun, mysqlRun} {
		var out snapshot
		err := run(func(p authoringPorts) error {
			if _, err := p.scenarios.Create(ctx, input); err != nil {
				return err
			}
			v := draft
			version, err := p.iterations.NextVersion(ctx, tenant.String(), parent.String())
			if err != nil {
				return err
			}
			v.Version = version
			if _, err := p.iterations.Create(ctx, v); err != nil {
				return err
			}
			for _, rule := range rules {
				if _, err := p.rules.Create(ctx, rule); err != nil {
					return err
				}
			}
			updated, err := p.rules.GetByID(ctx, tenant.String(), parent.String(), iteration.String(), rules[0].ID)
			if err != nil {
				return err
			}
			updated.ScoreModifier = -5
			updated.Description = "changed"
			if _, err := p.rules.Update(ctx, updated); err != nil {
				return err
			}
			if _, err := p.iterations.Commit(ctx, tenant.String(), parent.String(), iteration.String(), at); err != nil {
				return err
			}
			out.Scenario, err = p.scenarios.GetByID(ctx, tenant.String(), parent.String())
			if err != nil {
				return err
			}
			out.Iterations, err = p.iterations.ListByScenario(ctx, tenant.String(), parent.String())
			if err != nil {
				return err
			}
			out.Rules, err = p.rules.ListByIteration(ctx, tenant.String(), parent.String(), iteration.String())
			if err != nil {
				return err
			}
			out.Groups, err = p.rules.ListRuleGroupsByScenario(ctx, tenant.String(), parent.String())
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, out)
	}
	// JSON semantic comparison preserves exact strings, signed numbers, nullable
	// thresholds/IDs and evidence fields without comparing server JSON formatting.
	canonical := func(v any) any {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var result any
		if err := decoder.Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if !reflect.DeepEqual(canonical(outputs[0]), canonical(outputs[1])) {
		t.Fatalf("source/target authoring corpus differs:\nPG=%#v\nMySQL=%#v", outputs[0], outputs[1])
	}
}
