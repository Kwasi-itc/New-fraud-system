package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type combinedVerifier []auth.TokenVerifier

func (v combinedVerifier) Verify(raw string) (auth.Principal, error) {
	for _, one := range v {
		if p, err := one.Verify(raw); err == nil {
			return p, nil
		}
	}
	return auth.Principal{}, errors.New("untrusted token")
}
func TestClientInboxGrantAndTenantBoundCaseReads(t *testing.T) {
	db := authtest.SharedDatabase(t)
	ctx := context.Background()
	tenant, other, client, inbox, otherInbox, caseID, otherCase := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	human, adminToken := authtest.HumanIdentity(t, tenant, "inboxes:manage", "cases:access-all", "cases:read")
	actor, err := human.Verify(adminToken)
	if err != nil {
		t.Fatal(err)
	}
	external, clientToken := authtest.ClientIdentity(t, tenant, client, "cases:read")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []uuid.UUID{tenant, other} {
		exec(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'ACL',$2,'active',now(),now())`, id, "t_"+id.String()[:8])
	}
	exec(`INSERT INTO identity.users(id,display_name) VALUES($1,'Admin')`, actor.Subject)
	exec(`INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,ARRAY['tenant_admin'])`, tenant, actor.Subject)
	exec(`INSERT INTO identity.sessions(id,digest,tenant_id,user_id,expires_at,idle_until) VALUES($1,$2,$3,$4,now()+interval '1 hour',now()+interval '1 hour')`, actor.SessionID, []byte(actor.SessionID.String()), tenant, actor.Subject)
	exec(`INSERT INTO identity.clients(tenant_id,id,name,scopes) VALUES($1,$2,'Customer',ARRAY['cases:read'])`, tenant, client)
	for i, box := range []uuid.UUID{inbox, otherInbox} {
		caseTenant := tenant
		if i == 1 {
			caseTenant = other
		}
		exec(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Inbox',now(),now())`, box, caseTenant)
		id := caseID
		if i == 1 {
			id = otherCase
		}
		exec(`INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES($1,$2,$3,'Secret-free summary','pending','unset','decision',now(),now())`, id, caseTenant, box)
	}
	router := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), db, RouterConfig{Verifier: combinedVerifier{human, external}})
	request := func(method, path, token string, want int) []byte {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s: %d want %d %s", method, path, w.Code, want, w.Body.String())
		}
		return w.Body.Bytes()
	}
	base := "/v1/tenants/" + tenant.String()
	grants := base + "/clients/" + client.String() + "/inboxes/"
	request("GET", base+"/cases/"+caseID.String(), clientToken, 403)
	request("PUT", grants+inbox.String(), clientToken, 403)
	request("PUT", grants+otherInbox.String(), adminToken, 403)
	request("PUT", grants+inbox.String(), adminToken, 204)
	request("PUT", grants+inbox.String(), adminToken, 204)
	body := request("GET", base+"/cases", clientToken, 200)
	var page struct {
		Cases []store.ClientCaseSummary `json:"cases"`
	}
	if json.Unmarshal(body, &page) != nil || len(page.Cases) != 1 || page.Cases[0].ID != caseID {
		t.Fatal("client page", string(body))
	}
	request("GET", base+"/cases/"+caseID.String(), clientToken, 200)
	request("GET", "/v1/tenants/"+other.String()+"/cases/"+otherCase.String(), clientToken, 403)
	request("DELETE", grants+inbox.String(), adminToken, 204)
	request("GET", base+"/cases/"+caseID.String(), clientToken, 403)
	var events int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.inbox_events WHERE tenant_id=$1`, tenant).Scan(&events); err != nil || events != 2 {
		t.Fatal("grant audit should record actual changes once", events, err)
	}
	// The grant bound is atomic under concurrent requests for the same client.
	boxes := make([]uuid.UUID, 257)
	for i := range boxes {
		boxes[i] = uuid.New()
		exec(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,$3,now(),now())`, boxes[i], tenant, "Bounded "+boxes[i].String())
		if i < 255 {
			exec(`INSERT INTO case_manager.client_inbox_access(tenant_id,client_id,inbox_id,granted_by) VALUES($1,$2,$3,$4)`, tenant, client, boxes[i], actor.Subject)
		}
	}
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for _, box := range boxes[255:] {
		wg.Add(1)
		go func(box uuid.UUID) {
			defer wg.Done()
			req := httptest.NewRequest("PUT", grants+box.String(), strings.NewReader("{}"))
			req.Header.Set("Authorization", "Bearer "+adminToken)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			results <- w.Code
		}(box)
	}
	wg.Wait()
	close(results)
	statuses := map[int]int{}
	for status := range results {
		statuses[status]++
	}
	if statuses[204] != 1 || statuses[409] != 1 {
		t.Fatal("grant bound raced", statuses)
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.client_inbox_access WHERE tenant_id=$1 AND client_id=$2`, tenant, client).Scan(&count); err != nil || count != 256 {
		t.Fatal(count, err)
	}
	// Representative worst-case grant count, with 100 records per inbox.
	exec(`INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at)
 SELECT md5(a.inbox_id::text||g::text)::uuid,a.tenant_id,a.inbox_id,'Plan fixture','pending','unset','decision',now(),now()
 FROM case_manager.client_inbox_access a CROSS JOIN generate_series(1,100) g WHERE a.tenant_id=$1 AND a.client_id=$2`, tenant, client)
	exec(`ANALYZE case_manager.cases; ANALYZE case_manager.client_inbox_access`)
	plan, err := db.Query(ctx, "EXPLAIN (ANALYZE,BUFFERS) "+store.ClientCaseListSQL, tenant, client, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	for plan.Next() {
		var line string
		if err := plan.Scan(&line); err != nil {
			t.Fatal(err)
		}
		t.Log(line)
	}
	if err := plan.Err(); err != nil {
		t.Fatal(err)
	}

}
