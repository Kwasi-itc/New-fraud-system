package mysqlruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/localdelivery"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
	"os"
	"time"
)

func OpenLocalProvider(ctx context.Context) (*localdelivery.MultiProvider, *sql.DB, error) {
	if _, err := localdev.Origin(os.Getenv("LOCAL_MYSQL_NO_AUTH"), os.Getenv("APP_ORIGIN")); err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile("/runtime/config.json")
	if err != nil {
		return nil, nil, err
	}
	var c LocalConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, nil, err
	}
	if c.TenantID == uuid.Nil || c.DeliveryPlan.Owner != localdelivery.Owner || c.DeliveryPlan.Database != "fraud" {
		return nil, nil, errors.New("explicit shared local provider scope required")
	}
	password, err := LocalSecret("decision_provider_password")
	if err != nil {
		return nil, nil, err
	}
	token, err := LocalSecret("decision_provider_token")
	if err != nil {
		return nil, nil, err
	}
	db, err := ec2test.Open(ctx, mysqlstore.Config{Address: "mysql:3306", Database: "fraud", User: LocalProviderUser, Password: password, AllowInsecureTransport: true, InterpolateParameters: true, MaxOpen: 4, MaxIdle: 2, ConnectTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, LockWaitTimeout: 3 * time.Second})
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*localdelivery.MultiProvider, *sql.DB, error) { db.Close(); return nil, nil, err }
	runner, err := migrations.NewForOwner(db, localdelivery.Owner, c.DeliveryPlan)
	if err != nil {
		return fail(err)
	}
	history, err := runner.Status(ctx)
	if err != nil {
		return fail(err)
	}
	if len(history) != len(c.DeliveryPlan.Steps) {
		return fail(migrations.ErrLineage)
	}
	for _, r := range history {
		if r.State != "applied" {
			return fail(migrations.ErrLineage)
		}
	}
	provider, err := localdelivery.NewMultiProvider(db, "fraud", token)
	if err != nil {
		return fail(err)
	}
	mode := os.Getenv("LOCAL_CALLBACK_MODE")
	if mode != "" && mode != "accept" && mode != "reject" {
		return fail(errors.New("invalid local callback receiver policy"))
	}
	provider.SetCallbackPolicy(mode == "reject")
	return provider, db, nil
}
func localProviderAuthority(tenant uuid.UUID) localdelivery.Authorize {
	return func(ctx context.Context, tx *sql.Tx, id uuid.UUID, _ bool) error {
		if id != tenant {
			return localdelivery.ErrRequest
		}
		var active bool
		if err := tx.QueryRowContext(ctx, "SELECT status='active' FROM fraud.data_model_tenants WHERE id=?", mysqlstore.UUID(id)).Scan(&active); err != nil {
			return err
		}
		if !active {
			return localdelivery.ErrRequest
		}
		return nil
	}
}
func (a *Application) LocalDeliveryReceipts(ctx context.Context, kind ports.JobKind, resource uuid.UUID) ([]localdelivery.Delivery, error) {
	return a.LocalDeliveryReceiptsForTenant(ctx, a.tenants[0], kind, resource)
}
func (a *Application) LocalDeliveryReceiptsForTenant(ctx context.Context, tenant uuid.UUID, kind ports.JobKind, resource uuid.UUID) ([]localdelivery.Delivery, error) {
	if resource == uuid.Nil {
		return nil, localdelivery.ErrRequest
	}
	s, err := localdelivery.NewStore(a.runtime.deps.DB, a.runtime.deps.Database, tenant, localProviderAuthority(tenant))
	if err != nil {
		return nil, err
	}
	return s.Read(ctx, kind, resource)
}
