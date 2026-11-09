package integrationtest

import (
	"database/sql"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/httpapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/mysql"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

// MySQLWorkflowReceiptOwner uses installed actual owner tables and mutations.
// The caller installs the participating owner migrations in a generated fixture.
func MySQLWorkflowReceiptOwner(t *testing.T, db *sql.DB, database, token string, tenants ...uuid.UUID) http.Handler {
	t.Helper()
	if !strings.HasPrefix(database, "decision_target_") && !strings.HasPrefix(database, "decision_component_fixture_") {
		t.Fatal("generated decision fixture required")
	}
	repo, err := store.NewWorkflowStore(db, database, fixtureIDs{}, fixtureClock{})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := service.NewWorkflowReceiptService(repo, fixtureClock{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := httpapi.NewWorkflowReceiptRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), owner, httpapi.AuthConfig{Mode: "token", Token: token, TenantIDs: tenants}, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
