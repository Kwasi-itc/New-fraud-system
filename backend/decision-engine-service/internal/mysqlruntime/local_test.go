package mysqlruntime

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/localtenants"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLocalDecisionHostRequiresIndependentOptIn(t *testing.T) {
	t.Setenv("LOCAL_MYSQL_NO_AUTH", "")
	t.Setenv("APP_ORIGIN", "http://localhost:8082")
	if app, db, err := OpenLocal(context.Background(), false); err == nil || app != nil || db != nil {
		t.Fatal("local host accepted implicit opt-in", err)
	}
	t.Setenv("LOCAL_MYSQL_NO_AUTH", "1")
	t.Setenv("APP_ORIGIN", "http://remote.example")
	if app, db, err := OpenLocal(context.Background(), false); err == nil || app != nil || db != nil {
		t.Fatal("local host accepted foreign public origin", err)
	}
}

func TestLocalReviewCredentialsAreTenantScopedAndPreserveLegacyFixture(t *testing.T) {
	raw := strings.Repeat("a", 64)
	first, second := uuid.New(), uuid.New()
	a, err := localTenantReviewCredential(raw, first, first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := localTenantReviewCredential(raw, second, first)
	if err != nil || a == b {
		t.Fatal("shared cross-tenant credential", err)
	}
	again, err := localTenantReviewCredential(raw, second, first)
	if err != nil || b != again {
		t.Fatal("unstable credential", err)
	}
	if _, err := localTenantReviewCredential(raw, uuid.Nil, first); err == nil {
		t.Fatal("nil tenant credential")
	}
}

func TestLocalWorkerLoopsRespectFixedSlotBudget(t *testing.T) {
	registry, err := localtenants.New(new(sql.DB), "fraud")
	if err != nil {
		t.Fatal(err)
	}
	runtime := &Runtime{policy: localPolicy()}
	loops, err := runtime.localWorkerLoops(registry)
	if err != nil || len(loops) != 15 {
		t.Fatal(len(loops), err)
	}
	runtime.policy.MaxWorkerSlots = 14
	if _, err := runtime.localWorkerLoops(registry); err == nil {
		t.Fatal("worker budget was ignored")
	}
}

func TestLocalDynamicModelsObservePublicationChanges(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintf(w, `{"data_model":{"revision_id":"revision-%d","tables":{}}}`, calls)
	}))
	defer server.Close()
	models := localModels{datamodel.NewHTTPClient(server.URL, time.Second)}
	tenant := uuid.NewString()
	for _, revision := range []string{"revision-1", "revision-2"} {
		model, err := models.GetTenantModel(context.Background(), tenant)
		if err != nil || model.RevisionID != revision {
			t.Fatal("stale published model", model.RevisionID, err)
		}
	}
}

func TestLocalDecisionPolicyUsesBoundedLocalDelivery(t *testing.T) {
	p := localPolicy()
	if p.Evaluation.DeferExternalEffects || p.Evaluation.DeferAdditionalScoring || !p.Evaluation.PublishOutboxEvents || p.Admission.DisableCallbacks || p.Admission.CallbackAllowedURL != "http://decision-local-providers:8090/callbacks" {
		t.Fatal("local provider capabilities or callback destination are not explicit")
	}
	if p.MaxWorkerTenants < 2 || p.MaxWorkerSlots > 16 {
		t.Fatal("unbounded local worker scope")
	}
}
