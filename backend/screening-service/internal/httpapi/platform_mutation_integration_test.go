package httpapi

import (
	"context"
	"encoding/json"
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOwnerReviewUsesSignedActorAndCurrentCapability(t *testing.T) {
	db := authtest.SharedDatabase(t)
	ctx := context.Background()
	tenant, run := uuid.New(), uuid.New()
	match := uuid.NewString()
	verifier, token := authtest.HumanIdentity(t, tenant, "screening:review")
	p, err := verifier.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Review',$2,'active',now(),now())`, tenant, "t_"+tenant.String()[:8])
	exec(`INSERT INTO identity.users(id,display_name) VALUES($1,'Operator')`, p.Subject)
	exec(`INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,ARRAY['tenant_admin'])`, tenant, p.Subject)
	exec(`INSERT INTO identity.sessions(id,digest,tenant_id,user_id,expires_at,idle_until) VALUES($1,$2,$3,$4,now()+interval '1 hour',now()+interval '1 hour')`, p.SessionID, []byte(p.SessionID.String()), tenant, p.Subject)
	exec(`INSERT INTO screening.screenings(id,tenant_id,provider,object_type,object_id,status,created_at,updated_at) VALUES($1,$2,'test','customer','one','awaiting_review',now(),now())`, run, tenant)
	exec(`INSERT INTO screening.screening_matches(id,tenant_id,screening_id,entity_id,provider,status,name,created_at,updated_at) VALUES($1,$2,$3,'entity','test','pending','Match',now(),now())`, match, tenant, run)
	router := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), db, RouterConfig{Verifier: verifier})
	review := func(want int) {
		t.Helper()
		r := httptest.NewRequest("POST", "/v1/tenants/"+tenant.String()+"/screening-matches/"+match+"/review", strings.NewReader(`{"status":"no_hit","comment":"Evidence checked","reviewer_id":"forged-administrator"}`))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("review status=%d want %d: %s", w.Code, want, w.Body.String())
		}
	}
	review(200)
	var actor string
	if err := db.QueryRow(ctx, `SELECT author_id FROM screening.screening_match_comments WHERE tenant_id=$1 AND match_id=$2`, tenant, match).Scan(&actor); err != nil || actor != p.Subject {
		t.Fatal("review accepted caller-supplied identity", actor, err)
	}
	var payload map[string]any
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT payload FROM screening.case_event_outbox WHERE tenant_id=$1`, tenant).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload["reviewer_id"] != p.Subject {
		t.Fatal("callback actor was forged", err)
	}
	exec(`UPDATE identity.sessions SET revoked_at=now() WHERE id=$1`, p.SessionID)
	review(403)
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.case_event_outbox WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 1 {
		t.Fatal("revoked review wrote another event", count, err)
	}
}
