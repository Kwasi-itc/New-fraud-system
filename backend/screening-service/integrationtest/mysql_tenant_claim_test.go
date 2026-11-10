package integrationtest

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	api "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/httpapi"
	runtime "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/mysqlruntime"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestMySQLScreeningDispatchTenantActivation(t *testing.T) {
	f := fixtureMySQL(t)
	svc := api.MySQLService(f.store, api.MySQLDependencies{Provider: runtime.LocalProvider{}, Inbox: f.store})
	source, e := svc.CreateManualSearch(f.ctx, f.tenant.String(), f.user.String(), f.manualInput())
	must(t, e)
	_, e = f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.identity_tenants SET active=FALSE WHERE id=?", mysqlstore.UUID(f.tenant))
	must(t, e)
	pending, e := f.store.Screenings().ListByStatus(f.ctx, "pending", 25)
	must(t, e)
	if len(pending) != 0 {
		t.Fatal("inactive tenant work remained eligible")
	}
	claimed, e := f.store.Screenings().Claim(f.ctx, f.tenant.String(), source.ID, uuid.NewString(), time.Now().UTC())
	must(t, e)
	if claimed {
		t.Fatal("inactive tenant screening was claimed")
	}
	_, e = f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.identity_tenants SET active=TRUE WHERE id=?", mysqlstore.UUID(f.tenant))
	must(t, e)
	claimed, e = f.store.Screenings().Claim(f.ctx, f.tenant.String(), source.ID, uuid.NewString(), time.Now().UTC())
	must(t, e)
	if !claimed {
		t.Fatal("reactivated tenant could not resume work")
	}
	_, e = f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.screening_screenings SET processing_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)),projection_version=projection_version+1 WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(source.ID)))
	must(t, e)
	_, e = f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.identity_tenants SET active=FALSE WHERE id=?", mysqlstore.UUID(f.tenant))
	must(t, e)
	recoverable, e := f.store.Screenings().ListRecoverable(f.ctx, time.Now().UTC(), 25)
	must(t, e)
	if len(recoverable) != 0 {
		t.Fatal("inactive tenant recovery was eligible")
	}
}
