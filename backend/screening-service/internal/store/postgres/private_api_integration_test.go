package postgres_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/httpapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/privatelists"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestPrivateListSignedAPIUsesApprovedCurrentRolesAndSeparateReviewer(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	pub, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	verifier, e := identity.NewVerifier(map[string][]byte{"private-test": pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})}, "private-test", "private-test")
	if e != nil {
		t.Fatal(e)
	}
	sign := func(p identity.Principal) string {
		t.Helper()
		claims := jwt.MapClaims{"sub": p.Subject, "tenant_id": p.TenantID.String(), "sid": p.SessionID.String(), "authz_version": p.AuthzVersion, "roles": []string{"tenant_admin"}, "iss": "private-test", "aud": "private-test", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = "private-test"
		raw, e := token.SignedString(key)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	api := httptest.NewServer(httpapi.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), f.db, httpapi.RouterConfig{InvestigatorVerifier: verifier, PrivateListWriter: f.db, PrivateListPolicy: privatelists.TenantAdministratorPolicy(), AuthMode: "disabled"}))
	defer api.Close()
	call := func(method, path, token string, in any, want int) map[string]any {
		t.Helper()
		var raw []byte
		if in != nil {
			raw, _ = json.Marshal(in)
		}
		request, _ := http.NewRequest(method, api.URL+"/v1/private-lists"+path, bytes.NewReader(raw))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response, e := api.Client().Do(request)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != want {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.StatusCode, want, body)
		}
		out := map[string]any{}
		json.Unmarshal(body, &out)
		return out
	}
	author := f.a.Principal()
	token := sign(author)
	create := map[string]any{"operation_id": uuid.NewString(), "name": "API fictional list", "purpose": "Synthetic integration", "rights_reference": "Own fictional records"}
	call("POST", "", token, create, 403) // A signed role claim cannot grant DB permissions.
	call("GET", "/capabilities", "operator-token-is-not-a-platform-session", nil, 401)
	if _, e = f.db.Exec(ctx, `UPDATE identity.memberships SET roles=ARRAY['tenant_admin','case_investigator'] WHERE tenant_id=ANY($1::uuid[])`, []uuid.UUID{f.a.Tenant(), f.b.Tenant()}); e != nil {
		t.Fatal(e)
	}
	list := call("POST", "", token, create, 200)["id"].(string)
	forged := map[string]any{"operation_id": uuid.NewString(), "name": "Wrong namespace", "purpose": "Synthetic", "rights_reference": "Own fixture", "tenant_id": f.b.Tenant().String()}
	call("POST", "", token, forged, 400)
	item := call("POST", "/"+list+"/imports", token, map[string]any{"operation_id": uuid.NewString(), "csv": "record_id,name,entity_type,source_reference\n001,Fictional API Person,Person,own fixture\n"}, 200)
	importID := item["id"].(string)
	path := "/" + list + "/imports/" + importID
	call("POST", path+"/approve", token, map[string]any{}, 403)
	call("GET", path, sign(f.b.Principal()), nil, 404)
	other := call("GET", "", sign(f.b.Principal()), nil, 200)
	if len(other["lists"].([]any)) != 0 {
		t.Fatal("foreign list metadata leaked")
	}
	reviewer := identity.Principal{Subject: uuid.NewString(), TenantID: author.TenantID, SessionID: uuid.New(), AuthzVersion: 1}
	if _, e = f.db.Exec(ctx, `INSERT INTO identity.users(id,display_name) VALUES($1,'Second API administrator')`, reviewer.Subject); e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(ctx, `INSERT INTO identity.memberships(user_id,tenant_id,roles) VALUES($1,$2,ARRAY['tenant_admin'])`, reviewer.Subject, reviewer.TenantID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(ctx, `INSERT INTO identity.sessions(id,user_id,tenant_id,digest,expires_at,idle_until) VALUES($1,$2,$3,$4,now()+interval '1 hour',now()+interval '1 hour')`, reviewer.SessionID, reviewer.Subject, reviewer.TenantID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	reviewerToken := sign(reviewer)
	approved := call("POST", path+"/approve", reviewerToken, map[string]any{}, 200)
	if approved["status"] != "published" {
		t.Fatal(approved)
	}
	call("GET", path+"/diff", reviewerToken, nil, 200)
	if _, e = f.db.Exec(ctx, `UPDATE identity.memberships SET active=false,version=version+1 WHERE user_id=$1 AND tenant_id=$2`, reviewer.Subject, reviewer.TenantID); e != nil {
		t.Fatal(e)
	}
	call("POST", path+"/approve", reviewerToken, map[string]any{}, 403)
}
