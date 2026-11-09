package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestActualDecisionServicePlatformReadParity(t *testing.T) {
	actualDecisionServiceParityMode(t, false, true)
}

func realPlatformGoldenCorpus() []scalarGoldenCase {
	tag := func(v string) map[string]any { return scalarNamed("record_has_tag", map[string]any{"tag": v}) }
	risk := scalarCall("record_risk_level")
	flag := func(ip, v string) map[string]any {
		return scalarNamed("has_ip_flag", map[string]any{"ip": ip, "flag": v})
	}
	return []scalarGoldenCase{
		{name: "tag_present", formula: tag("Watch"), matched: true},
		{name: "tag_absent", formula: tag("Watch")},
		{name: "tag_exact_case", formula: tag("WATCH")},
		{name: "tag_foreign", formula: tag("Watch")},
		{name: "risk_present", formula: scalarCall("eq", risk, scalarConstant("high")), matched: true},
		{name: "risk_latest", formula: scalarCall("eq", risk, scalarConstant("high")), matched: true},
		{name: "risk_missing", formula: scalarCall("is_null", risk), matched: true},
		{name: "risk_foreign", formula: scalarCall("is_null", risk), matched: true},
		{name: "ip_present", formula: flag("192.0.2.1", "proxy"), matched: true},
		{name: "ip_absent", formula: flag("192.0.2.2", "proxy")},
		{name: "ip_flag_exact", formula: flag("192.0.2.1", "PROXY")},
		{name: "ip_foreign", formula: flag("192.0.2.3", "proxy")},
	}
}

func preparePlatformParity(t *testing.T, ctx context.Context, pg *pgxpool.Pool, f effectFixture, auth PlatformAuthorizer) evaluationPlatform {
	t.Helper()
	store, err := NewPlatformStore(f.db, f.database, f.tenant, 500, 2<<20, auth)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	foreign := uuid.New()
	for _, tenant := range []uuid.UUID{f.tenant, foreign} {
		target := store
		if tenant == foreign {
			// Explicit privileged isolated seeding authority; evaluation uses the real session.
			target, err = NewPlatformStore(f.db, f.database, tenant, 500, 2<<20, allowPlatformFixture)
			if err != nil {
				t.Fatal(err)
			}
		}
		check := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		names := []string{"tag_present", "tag_exact_case"}
		if tenant == foreign {
			names = []string{"tag_foreign"}
		}
		for _, name := range names {
			v := platform.RecordTag{ID: uuid.NewString(), TenantID: tenant.String(), ObjectType: "wallets", ObjectID: name, Tag: "Watch", CreatedAt: at}
			_, err = postgres.NewRecordTagRepository(pg).Create(ctx, v)
			check(err)
			check(target.Mutate(ctx, func(r PlatformRepositories) error { _, e := r.RecordTags.Create(ctx, v); return e }))
		}
		names = []string{"risk_present", "risk_latest"}
		if tenant == foreign {
			names = []string{"risk_foreign"}
		}
		for _, name := range names {
			for i, level := range []string{"low", "high"} {
				v := platform.RiskSnapshot{ID: uuid.NewString(), TenantID: tenant.String(), ObjectType: "wallets", ObjectID: name, RiskLevel: level, CreatedAt: at.Add(time.Duration(i) * time.Second)}
				_, err = postgres.NewRiskSnapshotRepository(pg).Create(ctx, v)
				check(err)
				check(target.Mutate(ctx, func(r PlatformRepositories) error { _, e := r.RiskSnapshots.Create(ctx, v); return e }))
			}
		}
		ip := "192.0.2.1"
		if tenant == foreign {
			ip = "192.0.2.3"
		}
		v := platform.IPFlag{ID: uuid.NewString(), TenantID: tenant.String(), IPAddress: ip, Flag: "proxy", CreatedAt: at}
		_, err = postgres.NewIPFlagRepository(pg).Create(ctx, v)
		check(err)
		check(target.Mutate(ctx, func(r PlatformRepositories) error { _, e := r.IPFlags.Create(ctx, v); return e }))
	}
	return evaluationPlatform{store}
}

func TestActualDecisionServicePlatformSQLFailuresAbort(t *testing.T) {
	actualDecisionServiceFixture(t, false, true, func(ctx context.Context, f effectFixture, _ *DecisionUnitOfWorkStore, svc service.DecisionService, parent uuid.UUID) {
		cases := append(realListGoldenCorpus(), realPlatformGoldenCorpus()...)
		for _, tc := range []struct{ name, table string }{{"name_hit", "custom_list_entries"}, {"tag_present", "record_tags"}, {"risk_present", "risk_snapshots"}, {"ip_present", "ip_flags"}} {
			t.Run(tc.name, func(t *testing.T) {
				index := -1
				for i, c := range cases {
					if c.name == tc.name {
						index = i
					}
				}
				if index < 0 {
					t.Fatal("missing golden case")
				}
				table := "`" + f.database + "`.decision_engine_" + tc.table
				// Rename only this generated disposable fixture's table; a SQL error must
				// not become an absent value or a successful no-match decision.
				if _, err := f.db.ExecContext(ctx, "RENAME TABLE "+table+" TO "+table+"_unavailable"); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := f.db.ExecContext(ctx, "RENAME TABLE "+table+"_unavailable TO "+table); err != nil {
						t.Error(err)
					}
				}()
				result, err := svc.EvaluateScenario(ctx, f.tenant.String(), parent.String(), service.DecisionEvaluationRequest{ObjectID: tc.name, ObjectType: "wallets", Fields: map[string]any{"amount": index}})
				if err == nil || result.Decision != nil || len(result.RuleExecutions) != 0 {
					t.Fatalf("read failure exposed output: %+v %v", result, err)
				}
				for _, table := range []string{"decision_engine_decisions", "decision_engine_rule_executions", "decision_engine_workflow_executions", "decision_engine_screening_executions", "decision_engine_scoring_requests", "decision_engine_outbox_events"} {
					if n := executionCount(t, f.platformFixture, table); n != 0 {
						t.Fatalf("%s persisted %d rows", table, n)
					}
				}
			})
		}
	})
}
