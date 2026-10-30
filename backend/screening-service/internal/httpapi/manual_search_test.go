package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type manualAuthDB struct {
	allowed bool
	args    []any
}

func (db *manualAuthDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	db.args = args
	return manualAuthRow{db.allowed}
}

type manualAuthRow struct{ allowed bool }

func (r manualAuthRow) Scan(dest ...any) error { *dest[0].(*bool) = r.allowed; return nil }

func TestManualAuthorizationSignedTenantAndCurrentSession(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})
	verifier, err := identity.NewVerifier(map[string][]byte{"test": pub}, "test-issuer", "test-audience")
	if err != nil {
		t.Fatal(err)
	}
	tenant, actor, sid := uuid.New(), uuid.New(), uuid.New()
	db := &manualAuthDB{allowed: true}
	for _, tc := range []struct {
		name    string
		roles   []string
		session string
		allowed bool
		want    int
	}{
		{"investigator", []string{"case_investigator"}, sid.String(), true, 0},
		{"admin", []string{"case_manager_admin"}, sid.String(), true, 0},
		{"role denied", []string{"viewer"}, sid.String(), true, 403},
		{"revoked", []string{"case_investigator"}, sid.String(), false, 403},
		{"no session", []string{"case_investigator"}, "", true, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := identity.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: actor.String(), Issuer: "test-issuer", Audience: jwt.ClaimStrings{"test-audience"}, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}, TenantID: tenant.String(), SessionID: tc.session, AuthzVersion: 1, Roles: tc.roles}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			token.Header["kid"] = "test"
			raw, err := token.SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/v1/manual-screenings/unused", nil)
			req.Header.Set("Authorization", "Bearer "+raw)
			db.allowed = tc.allowed
			p, err := manualAuthorization(db, verifier, true)(req)
			if tc.want == 0 {
				if err != nil || p.TenantID != tenant || p.Subject != actor.String() || db.args[1] != tenant {
					t.Fatalf("identity mismatch: %+v %v", p, err)
				}
				return
			}
			var auth *identity.Error
			if !errors.As(err, &auth) || auth.Code != tc.want {
				t.Fatalf("got %v want %d", err, tc.want)
			}
		})
	}
}

type manualStub struct {
	tenant, actor string
	calls         int
}

func (s *manualStub) ListManualSearches(_ context.Context, tenant, actor string, _ screening.ManualHistoryFilter) (screening.ManualHistoryPage, error) {
	s.calls++
	s.tenant = tenant
	s.actor = actor
	return screening.ManualHistoryPage{Items: []screening.ManualHistoryItem{}}, nil
}

func (s *manualStub) CreateManualSearch(_ context.Context, tenant, actor string, in service.ManualSearchInput) (screening.Screening, error) {
	s.calls++
	s.tenant = tenant
	s.actor = actor
	if _, err := in.Request(actor); err != nil {
		return screening.Screening{}, err
	}
	return screening.Screening{ID: uuid.NewString(), Status: screening.StatusPending}, nil
}
func (s *manualStub) GetManualSearch(_ context.Context, tenant, actor, id string) (screening.Details, error) {
	s.calls++
	s.tenant = tenant
	s.actor = actor
	return screening.Details{}, pgx.ErrNoRows
}

func TestManualRoutesIdentityAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := uuid.New()
	stub := &manualStub{}
	router := gin.New()
	registerManualRoutes(router, func(*http.Request) (identity.Principal, error) {
		return identity.Principal{TenantID: tenant, Subject: "actor"}, nil
	}, stub)
	valid := `{"operation_id":"11111111-1111-4111-8111-111111111111","name":"Jane","entity_type":"Person","threshold":70,"limit":10}`
	for _, tc := range []struct {
		body string
		want int
	}{{valid, 202}, {strings.Replace(valid, `"name":"Jane"`, `"name":""`, 1), 400}, {strings.Replace(valid, `"name":"Jane"`, `"name":"Jane","tenant_id":"other"`, 1), 400}, {valid + `{}`, 400}} {
		req := httptest.NewRequest("POST", "/v1/manual-screenings", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != tc.want {
			t.Fatalf("%d: %s", res.Code, res.Body.String())
		}
	}
	if stub.tenant != tenant.String() || stub.actor != "actor" {
		t.Fatal("identity not propagated")
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest("GET", "/v1/manual-screenings/11111111-1111-4111-8111-111111111111", nil))
	if res.Code != 404 {
		t.Fatalf("expected nondisclosing 404, got %d", res.Code)
	}
}

func TestManualRoutesDeniedBeforeService(t *testing.T) {
	for _, code := range []int{401, 403, 503} {
		stub := &manualStub{}
		router := gin.New()
		registerManualRoutes(router, func(*http.Request) (identity.Principal, error) {
			return identity.Principal{}, &identity.Error{Code: code, Message: "denied"}
		}, stub)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest("POST", "/v1/manual-screenings", strings.NewReader(`{}`)))
		if res.Code != code || stub.calls != 0 {
			t.Fatal("authorization did not stop service")
		}
	}
}

func TestManualHistoryHTTPBoundary(t *testing.T) {
	tenant := uuid.New()
	stub := &manualStub{}
	router := gin.New()
	registerManualRoutes(router, func(*http.Request) (identity.Principal, error) {
		return identity.Principal{TenantID: tenant, Subject: "actor"}, nil
	}, stub)
	for _, tc := range []struct {
		query  string
		status int
	}{{"?limit=5&entity_type=Person", 200}, {"?tenant_id=foreign", 400}, {"?creator=foreign", 400}, {"?cursor=invalid", 400}} {
		before := stub.calls
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest("GET", "/v1/manual-screenings"+tc.query, nil))
		if res.Code != tc.status {
			t.Fatal(res.Code, res.Body.String())
		}
		if tc.status == 400 && stub.calls != before {
			t.Fatal("invalid history reached repository")
		}
	}
	if stub.tenant != tenant.String() || stub.actor != "actor" {
		t.Fatal("history identity not derived from auth")
	}
	denied := gin.New()
	registerManualRoutes(denied, func(*http.Request) (identity.Principal, error) {
		return identity.Principal{}, &identity.Error{Code: 403, Message: "denied"}
	}, stub)
	before := stub.calls
	res := httptest.NewRecorder()
	denied.ServeHTTP(res, httptest.NewRequest("GET", "/v1/manual-screenings", nil))
	if res.Code != 403 || stub.calls != before {
		t.Fatal("history bypassed authorization")
	}
}

func TestManualProjectionRedactsAndSorts(t *testing.T) {
	view := manualView(screening.Details{Screening: screening.Screening{Provider: "opensanctions", Partial: true, LastError: "secret-token"}, Matches: []screening.Match{
		{EntityID: "b", Score: .2, Payload: json.RawMessage(`{"properties":{"country":["gb"],"secret":["secret-token"]}}`)}, {EntityID: "a", Score: .9, Status: screening.MatchStatusConfirmedHit},
	}})
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "secret-token") {
		t.Fatal("private fields exposed")
	}
	if view["matches"].([]gin.H)[0]["entity_id"] != "a" || view["partial"] != true {
		t.Fatal("result semantics lost")
	}
	if view["provider"] != "opensanctions" || view["matches"].([]gin.H)[0]["status"] != screening.MatchStatusConfirmedHit {
		t.Fatal("report provenance or review status lost")
	}
}
