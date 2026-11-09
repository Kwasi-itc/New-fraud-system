package mysqlruntime

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
	"github.com/google/uuid"
	"testing"
)

func TestLocalOwnerRejectsForeignTenantBeforeDatabaseAccess(t *testing.T) {
	tenant := uuid.New()
	s := &Service{Config: Config{Authority: "local-mysql-development", TenantID: tenant, ObjectType: "transactions"}}
	for _, scope := range []struct {
		tenant uuid.UUID
		object string
	}{{uuid.New(), "transactions"}, {tenant, "foreign_table"}} {
		if _, err := s.owner(context.Background(), scope.tenant, scope.object); !errors.Is(err, service.ErrForbidden) {
			t.Fatalf("foreign owner access: %v", err)
		}
	}
}

func TestLocalScopeNeedsIndependentOptInAndRetainsTenantCredential(t *testing.T) {
	c := Config{Authority: "local-mysql-development", Address: "mysql:3306", Database: "fraud", User: "ingestion_local", Token: "server-internal-development-token", TenantID: uuid.New(), ObjectType: "transactions"}
	t.Setenv("LOCAL_MYSQL_NO_AUTH", "")
	t.Setenv("APP_ORIGIN", "http://localhost:3001")
	if err := c.validateScope(); err == nil {
		t.Fatal("accepted implicit local mode")
	}
	t.Setenv("LOCAL_MYSQL_NO_AUTH", "1")
	if err := c.validateScope(); err != nil {
		t.Fatal(err)
	}
	for _, alter := range []func(*Config){
		func(c *Config) { c.Database = "production" },
		func(c *Config) { c.Address = "production.example:3306" },
		func(c *Config) { c.User = "root" },
		func(c *Config) { c.TenantID = uuid.Nil },
		func(c *Config) { c.Token = "" },
	} {
		bad := c
		alter(&bad)
		if err := bad.validateScope(); err == nil {
			t.Fatal("unsafe local scope accepted")
		}
	}
	if err := c.Validate(); err == nil {
		t.Fatal("local mode bypassed missing budgets and migration artifacts")
	}
}

func TestIsolatedScopeDoesNotEnableLocalNoAuth(t *testing.T) {
	t.Setenv("LOCAL_MYSQL_NO_AUTH", "")
	t.Setenv("APP_ORIGIN", "")
	c := Config{Authority: "isolated-mysql", Address: "mysql:3306", Database: "ingestion_full_test_0123456789abcdef0123456789abcdef", User: "runtime", Token: "server-internal-development-token", TenantID: uuid.New(), ObjectType: "records"}
	if err := c.validateScope(); err != nil {
		t.Fatal(err)
	}
	c.Database = "fraud"
	if err := c.validateScope(); err == nil {
		t.Fatal("fixture mode admitted the persistent application database")
	}
}
