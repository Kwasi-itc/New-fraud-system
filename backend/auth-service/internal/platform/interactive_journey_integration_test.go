package platform

import (
	"bytes"
	"context"
	"encoding/json"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Starts with a provisioned OIDC identity's opaque session. Provider protocol
// validation is covered separately; this checks transport/session resolution,
// queue authorization and the actual owner review API in one shared database.
func TestInteractiveSessionQueueAndReviewJourney(t *testing.T) {
	cases, decisions := os.Getenv("CASE_MANAGER_INTEGRATION_BINARY"), os.Getenv("DECISION_INTEGRATION_BINARY")
	if cases == "" || decisions == "" {
		t.Skip("fresh Case Manager and Decision API binaries required")
	}
	db := authtest.SharedDatabase(t)
	store := Store{DB: db}
	ctx := context.Background()
	tenant, inbox, caseID, scenario, iteration, decision := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Interactive',$2,'active',now(),now())`, tenant, "t_"+tenant.String()[:8])
	user, opaque := provision(t, store, tenant, "investigator-journey", "case_investigator")
	exec(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Review',now(),now())`, inbox, tenant)
	exec(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.New(), tenant, inbox, user.String())
	exec(`INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES($1,$2,$3,'Interactive review','investigating','unset','decision',now(),now())`, caseID, tenant, inbox)
	exec(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Interactive','payment',now(),now())`, scenario, tenant)
	exec(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,created_at) VALUES($1,$2,$3,1,'committed',now())`, iteration, scenario, tenant)
	exec(`INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at) VALUES($1,$2,$3,$4,'one','payment','review',now())`, decision, tenant, scenario, iteration)
	exec(`INSERT INTO case_manager.case_decisions(id,tenant_id,case_id,decision_id,object_type,object_id,created_at) VALUES($1,$2,$3,$4,'payment','one',now())`, uuid.New(), tenant, caseID, decision)
	key, pub := clientTestKey(t)
	bff, bffToken := testBFF(t)
	owner := Server{Store: store, Verifier: bff, Key: key, KeyID: "interactive-platform", Issuer: auth.PlatformIssuer, Audience: auth.PlatformAudience, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	server := httptest.NewServer(owner.Handler())
	defer server.Close()
	request := func(method, url, token string, body []byte, want int) []byte {
		t.Helper()
		r, err := http.NewRequest(method, url, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s returned %d want %d: %s", method, url, response.StatusCode, want, data)
		}
		return data
	}
	sessionBody, _ := json.Marshal(map[string]string{"session": opaque})
	data := request("POST", server.URL+"/internal/session", bffToken, sessionBody, 200)
	var issued struct {
		Token string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &issued); err != nil || issued.Token == "" {
		t.Fatal("session token missing", err)
	}
	trust := auth.TrustedKey{ID: owner.KeyID, PublicKey: pub, Issuer: auth.PlatformIssuer, PrincipalTypes: []auth.PrincipalType{auth.PrincipalUser}, Audiences: []string{auth.PlatformAudience}}
	caseAPI := authtest.StartAPI(t, cases, db, "case-manager-service", []auth.TrustedKey{trust}, nil)
	decisionAPI := authtest.StartAPI(t, decisions, db, "decision-engine-service", []auth.TrustedKey{trust}, nil)
	data = request("GET", caseAPI+"/v1/tenants/"+tenant.String()+"/case-queue", issued.Token, nil, 200)
	var queue struct {
		Cases []struct {
			ID uuid.UUID `json:"id"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &queue); err != nil || len(queue.Cases) != 1 || queue.Cases[0].ID != caseID {
		t.Fatal("authorized queue missing", err, string(data))
	}
	path := decisionAPI + "/v1/investigator/tenants/" + tenant.String() + "/cases/" + caseID.String() + "/review"
	request("GET", path+"?resource_id="+decision.String(), issued.Token, nil, 200)
	command := auth.Command{OperationID: uuid.New(), ResourceID: decision.String(), ExpectedVersion: "0", Action: "approve", Findings: "Interactive evidence reviewed"}
	payload, _ := json.Marshal(command)
	first := request("POST", path, issued.Token, payload, 200)
	second := request("POST", path, issued.Token, payload, 200)
	if !bytes.Equal(first, second) {
		t.Fatal("idempotent review result changed")
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM core.investigator_reviews WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate receipt", count, err)
	}
	if err := store.Logout(ctx, opaque); err != nil {
		t.Fatal(err)
	}
	request("POST", path, issued.Token, payload, 403)
	request("GET", caseAPI+"/v1/tenants/"+tenant.String()+"/case-queue", issued.Token, nil, 200)
	request("POST", server.URL+"/internal/session", bffToken, sessionBody, 401)
}
