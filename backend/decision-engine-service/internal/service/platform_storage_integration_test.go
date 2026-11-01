package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/google/uuid"
)

func TestIntegrationPostgresPlatformRiskContract(t *testing.T) {
	ctx := context.Background()
	dsn := executionIntegrationDatabaseURL(t)
	runExecutionMetadataMigrations(t, dsn)
	db := executionIntegrationPool(t, ctx, dsn)
	defer db.Close()
	repo := storepostgres.NewRiskSnapshotRepository(db)
	tenant := uuid.NewString()
	at := time.Now().UTC().Truncate(time.Microsecond)
	for i, id := range []string{"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002"} {
		_, err := repo.Create(ctx, platform.RiskSnapshot{ID: id, TenantID: tenant, ObjectType: "wallets", ObjectID: "object", RiskLevel: []string{"low", "high"}[i], CreatedAt: at})
		if err != nil {
			t.Fatal(err)
		}
	}
	v, err := repo.GetByObject(ctx, tenant, "wallets", "object")
	if err != nil || v.RiskLevel != "high" {
		t.Fatal("equal-time latest risk is not deterministic", v, err)
	}
	if _, err := repo.GetByObject(ctx, uuid.NewString(), "wallets", "object"); !errors.Is(err, ports.ErrRiskSnapshotNotFound) {
		t.Fatal("cross tenant risk leaked", err)
	}
	value, err := asteval.EvaluateNode(ctx, ast.Node{Function: "record_risk_level"}, asteval.Runtime{TenantID: tenant, ObjectType: "wallets", ObjectID: "absent", RiskRepo: repo})
	if err != nil || value != nil {
		t.Fatal("confirmed absence not portable", value, err)
	}
	if _, err := db.Exec(ctx, "DROP TABLE core.risk_snapshots"); err != nil {
		t.Fatal(err)
	}
	if _, err := asteval.EvaluateNode(ctx, ast.Node{Function: "record_risk_level"}, asteval.Runtime{TenantID: tenant, ObjectType: "wallets", ObjectID: "absent", RiskRepo: repo}); err == nil {
		t.Fatal("storage error became no risk")
	}
}
