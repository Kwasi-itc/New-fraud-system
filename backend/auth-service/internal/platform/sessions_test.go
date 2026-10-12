package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSessionManagementIsolationAndRollback(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	user, raw := provision(t, s, tenant, "self", "case_investigator")
	_, foreignRaw := provision(t, s, tenant, "other", "tenant_admin")
	foreign, err := s.Resolve(ctx, foreignRaw)
	if err != nil {
		t.Fatal(err)
	}
	secondRaw, err := s.CreateSession(ctx, "https://provider.test", "self")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Resolve(ctx, secondRaw)
	if err != nil {
		t.Fatal(err)
	}
	otherTenant := uuid.New()
	otherID := uuid.New()
	otherRaw := secret()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Other',$2,'active',now(),now())`, []any{otherTenant, "tenant_" + strings.ReplaceAll(otherTenant.String(), "-", "")}},
		{`INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,ARRAY['case_investigator'])`, []any{otherTenant, user}},
		{`INSERT INTO identity.sessions(id,digest,tenant_id,user_id,expires_at,idle_until) VALUES($1,$2,$3,$4,now()+interval '1 hour',now()+interval '30 minutes')`, []any{otherID, digest(otherRaw), otherTenant, user}},
	} {
		if _, err = s.DB.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.Sessions(ctx, raw, uuid.Nil)
	if err != nil || len(items) != 2 {
		t.Fatal("session isolation", len(items), err)
	}
	currentCount := 0
	for _, item := range items {
		if item.Current {
			currentCount++
		}
		if item.ID == foreign.ID || item.ID == otherID {
			t.Fatal("session leaked")
		}
	}
	if currentCount != 1 {
		t.Fatal("current session marker")
	}
	for _, target := range []uuid.UUID{foreign.ID, otherID, uuid.New()} {
		if _, err = s.RevokeSessions(ctx, raw, target, false); !errors.Is(err, ErrDenied) {
			t.Fatal("foreign or unknown session accepted", err)
		}
	}
	if _, err = s.DB.Exec(ctx, `CREATE FUNCTION identity.fail_revoke_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test audit outage'; END $$; CREATE TRIGGER fail_revoke BEFORE INSERT ON identity.audit_events FOR EACH ROW EXECUTE FUNCTION identity.fail_revoke_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RevokeSessions(ctx, raw, second.ID, false); err == nil {
		t.Fatal("audit failure accepted")
	}
	if _, err = s.Resolve(ctx, secondRaw); err != nil {
		t.Fatal("failed audit did not roll back revocation", err)
	}
	if _, err = s.DB.Exec(ctx, `DROP TRIGGER fail_revoke ON identity.audit_events`); err != nil {
		t.Fatal(err)
	}
	if signedOut, err := s.RevokeSessions(ctx, raw, second.ID, false); err != nil || signedOut {
		t.Fatal("revoke other session", signedOut, err)
	}
	if allowed(t, s, second, false) {
		t.Fatal("old case credential still authorized")
	}
	if _, err = s.Resolve(ctx, secondRaw); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked secret accepted", err)
	}
	if _, err = s.RevokeSessions(ctx, raw, second.ID, false); err != nil {
		t.Fatal("retry failed", err)
	}
	var count int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM identity.audit_events WHERE target_id=$1 AND action='session.revoked'`, second.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate audit", count, err)
	}
	if signedOut, err := s.RevokeSessions(ctx, raw, uuid.Nil, true); err != nil || !signedOut {
		t.Fatal("revoke all", signedOut, err)
	}
	if _, err = s.Resolve(ctx, raw); !errors.Is(err, ErrDenied) {
		t.Fatal("caller survived revoke all", err)
	}
	for _, remaining := range []string{foreignRaw, otherRaw} {
		if _, err = s.Resolve(ctx, remaining); err != nil {
			t.Fatal("cross-account/tenant revocation", err)
		}
	}
	if _, err = s.RevokeSessions(ctx, raw, foreign.ID, false); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked caller accepted")
	}
}

func TestSessionRevocationWaitsForAuthorizedCaseWrite(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	_, raw := provision(t, s, tenant, "worker", "case_investigator")
	current, err := s.Resolve(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT identity.authorize_case_v1($1,$2,$3,$4,false,true)`, current.UserID.String(), tenant, current.ID, current.Version).Scan(&valid); err != nil || !valid {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, err := s.RevokeSessions(bounded, raw, uuid.Nil, true)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatal("revocation bypassed case lock", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if allowed(t, s, current, false) {
		t.Fatal("old credential survived committed revocation")
	}
}

func TestSessionsHTTPPaginationAndValidation(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	user, raw := provision(t, s, tenant, "self", "case_investigator")
	if _, err := s.DB.Exec(ctx, `INSERT INTO identity.sessions(id,digest,user_id,tenant_id,expires_at,idle_until) SELECT gen_random_uuid(),decode(md5(n::text),'hex'),$1,$2,now()+interval '1 hour',now()+interval '30 minutes' FROM generate_series(1,101) n`, user, tenant); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Server{Store: s, ProxyToken: strings.Repeat("x", 32), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}.Handler())
	defer server.Close()
	request := func(path, body string, status int) []byte {
		t.Helper()
		req, _ := http.NewRequest("POST", server.URL+"/internal/sessions/"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 32))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != status {
			t.Fatalf("%s status %d: %s", path, resp.StatusCode, data)
		}
		return data
	}
	page := request("list", `{"session":"`+raw+`"}`, 200)
	var out struct {
		Items []SessionInfo `json:"items"`
		After string        `json:"next_after"`
	}
	if err := json.Unmarshal(page, &out); err != nil || len(out.Items) != 100 || out.After == "" {
		t.Fatal("pagination", err, len(out.Items))
	}
	if strings.Contains(string(page), "digest") || strings.Contains(string(page), raw) || strings.Contains(string(page), "access_token") {
		t.Fatal("secret exposed")
	}
	page = request("list", `{"session":"`+raw+`","after":"`+out.After+`"}`, 200)
	if err := json.Unmarshal(page, &out); err != nil || len(out.Items) != 2 || out.After != "" {
		t.Fatal("second page", err, len(out.Items))
	}
	request("revoke", `{"session":"`+raw+`","all":true,"target":"`+uuid.NewString()+`"}`, 400)
	request("revoke", `{"session":"`+raw+`","user_id":"`+user.String()+`","all":true}`, 400)
	request("revoke", `{"session":"invalid","all":true}`, 403)
	request("list", `{"session":"`+raw+`","after":"invalid"}`, 400)
}
