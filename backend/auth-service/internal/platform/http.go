package platform

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Server struct {
	Store                   Store
	DefaultTenantID         uuid.UUID
	Provider                *oidcprovider.Provider
	Verifier                *auth.Verifier
	Replay                  *auth.ReplayCache
	TokenEndpointURL        string
	Key                     *rsa.PrivateKey
	KeyID, Issuer, Audience string
	Logger                  *slog.Logger
	Invitations             *InvitationService
	DebugLoginErrors        bool
	PolicyObserver          func([]auth.RoutePolicy)
}

func SigningKey(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("cannot read platform signing key")
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM(raw)
	if err != nil || key.N.BitLen() < 2048 {
		return nil, errors.New("invalid platform RSA signing key")
	}
	return key, nil
}
func (s Server) Handler() http.Handler {
	mux := newRouteRegistry()
	s.authorizationRoutes(mux)
	s.accountRoutes(mux)
	mux.HandlePublic("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	mux.HandlePublic("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		var version int
		if s.Store.DB.QueryRow(ctx, "SELECT max(version) FROM identity.schema_migrations").Scan(&version) != nil || version != SchemaVersion {
			http.Error(w, "not ready", 503)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleBootstrap("POST /internal/login/start", s.start)
	mux.HandleBootstrap("POST /internal/login/complete", s.complete)
	mux.HandleBootstrap("POST /internal/session", s.resolve)
	mux.HandleBootstrap("POST /internal/invitations/start", s.invitationStart)
	if s.PolicyObserver != nil {
		s.PolicyObserver(mux.Inventory())
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", "application/json")
		requestID := uuid.NewString()
		w.Header().Set("X-Request-ID", requestID)
		if strings.HasPrefix(r.URL.Path, "/internal/") {
			raw, e := auth.Bearer(r.Header.Get("Authorization"))
			if e != nil || s.Verifier == nil {
				http.Error(w, "unauthorized", 401)
				return
			}
			caller, e := s.Verifier.Verify(raw)
			if e != nil || !caller.IsService() || caller.Subject != "frontend-bff" || !caller.Can("auth:bootstrap") {
				http.Error(w, "unauthorized", 401)
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		defer func() {
			if recover() != nil {
				s.Logger.Error("auth request panic", "request_id", requestID)
				http.Error(w, "request failed", 500)
			}
		}()
		start := time.Now()
		mux.ServeHTTP(w, r.WithContext(ctx))
		s.Logger.Debug("auth request completed", "request_id", requestID, "method", r.Method, "duration_ms", time.Since(start).Milliseconds())
	})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		http.Error(w, "invalid request", 400)
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		http.Error(w, "invalid request", 400)
		return false
	}
	return true
}
func respond(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
func (s Server) fail(w http.ResponseWriter, err error) {
	s.failWithDetail(w, err, nil)
}
func (s Server) failWithDetail(w http.ResponseWriter, err, detail error) {
	status := 503
	msg := "Authentication service is unavailable."
	if errors.Is(err, ErrDenied) {
		status = 403
		msg = "An active account and required permission are needed."
	}
	if errors.Is(err, ErrConflict) {
		status = 409
		msg = ErrConflict.Error()
	}
	if s.DebugLoginErrors && detail != nil {
		msg = detail.Error()
	}
	w.WriteHeader(status)
	respond(w, map[string]string{"error": msg})
}
func (s Server) start(w http.ResponseWriter, r *http.Request) {
	s.Logger.Info("Starting OIDC login")
	l, err := s.Store.BeginLogin(r.Context(), s.Provider.Issuer())
	if err != nil {
		s.fail(w, err)
		return
	}
	u, err := s.Provider.AuthorizationURL(l.State, l.Nonce, l.Verifier)
	if err != nil {
		s.fail(w, err)
		return
	}
	respond(w, map[string]string{"url": u, "binding": l.Binding})
}
func (s Server) complete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		State   string `json:"state"`
		Binding string `json:"binding"`
		Code    string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	s.Logger.Info("Completing OIDC login")
	l, err := s.Store.ConsumeLogin(r.Context(), in.State, in.Binding, s.Provider.Issuer())
	if err != nil {
		detail := fmt.Errorf("consume login transaction (state, browser binding, issuer, or expiration mismatch): %w", err)
		s.Logger.Error("login transaction rejected", "error", detail)
		s.failWithDetail(w, err, detail)
		return
	}
	s.Logger.Info("Consuming login transaction")
	id, err := s.Provider.Exchange(r.Context(), in.Code, l.Verifier, l.Nonce)
	if err != nil {
		s.Logger.Error("OIDC exchange failed", "error", err)
		s.failWithDetail(w, ErrDenied, fmt.Errorf("OIDC exchange or ID-token validation failed: %w", err))
		return
	}
	s.Logger.Info("OIDC login completed", "issuer", id.Issuer, "subject", id.Subject, "name", id.Name, "email", id.Email, "email_verified", id.EmailVerified)
	var raw string
	if len(l.InvitationDigest) > 0 {
		if s.Invitations == nil {
			s.fail(w, ErrDenied)
			return
		}
		raw, err = s.Invitations.Accept(r.Context(), l.InvitationDigest, id)
	} else {
		raw, err = s.Store.CreateSessionForIdentity(r.Context(), id, s.DefaultTenantID)
	}
	if err != nil {
		detail := fmt.Errorf("create login session: %w", err)
		s.Logger.Error("login session creation failed", "error", detail)
		s.failWithDetail(w, err, detail)
		return
	}
	respond(w, map[string]string{"session": raw})
}

type sessionRequest struct {
	Session string `json:"session"`
}

func (s Server) resolve(w http.ResponseWriter, r *http.Request) {
	var in sessionRequest
	if !decode(w, r, &in) {
		return
	}
	v, err := s.Store.Resolve(r.Context(), in.Session)
	if err != nil {
		if errors.Is(err, ErrDenied) {
			w.WriteHeader(401)
			respond(w, map[string]string{"error": "Session expired or revoked."})
		} else {
			s.fail(w, err)
		}
		return
	}
	now := time.Now()
	expires := now.Add(time.Minute)
	if v.Expires.Before(expires) {
		expires = v.Expires
	}
	claims := auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: s.Issuer, Audience: jwt.ClaimStrings{s.Audience}, Subject: v.UserID.String(), ID: uuid.NewString(), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expires)}, PrincipalType: auth.PrincipalUser, TenantID: v.TenantID, Roles: v.Roles, Capabilities: v.Capabilities, SessionID: v.ID, AuthzVersion: v.Version}
	signed, err := auth.SignAccessToken(s.Key, s.KeyID, claims)
	if err != nil {
		s.fail(w, err)
		return
	}
	respond(w, map[string]any{"session": v, "access_token": signed})
}
func (s Server) accountLogout(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if _, err := s.Store.RevokeSessionsForPrincipal(r.Context(), p, p.SessionID, false); err != nil {
		s.authorizationError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s Server) accountSessions(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	after, err := optionalUUID(r.URL.Query().Get("after"))
	if err != nil {
		http.Error(w, "invalid cursor", 400)
		return
	}
	items, err := s.Store.SessionsForPrincipal(r.Context(), p, after)
	if err != nil {
		s.authorizationError(w, err)
		return
	}
	next := ""
	if len(items) > 100 {
		items = items[:100]
		next = items[99].ID.String()
	}
	respond(w, map[string]any{"items": items, "next_after": next})
}
func (s Server) accountRevokeSessions(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var in struct {
		Target uuid.UUID `json:"target"`
		All    bool      `json:"all"`
	}
	if !decode(w, r, &in) {
		return
	}
	if (in.All && in.Target != uuid.Nil) || (!in.All && in.Target == uuid.Nil) {
		http.Error(w, "Choose one session or all sessions", 400)
		return
	}
	current, err := s.Store.RevokeSessionsForPrincipal(r.Context(), p, in.Target, in.All)
	if err != nil {
		s.authorizationError(w, err)
		return
	}
	respond(w, map[string]bool{"signed_out": current})
}
func (s Server) accountMembers(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	after, err := optionalUUID(r.URL.Query().Get("after"))
	if err != nil {
		http.Error(w, "invalid cursor", 400)
		return
	}
	items, err := s.Store.FilteredMembersForPrincipal(r.Context(), p, after, r.URL.Query().Get("query"), r.URL.Query().Get("role"))
	if err != nil {
		s.authorizationError(w, err)
		return
	}
	next := ""
	if len(items) > 100 {
		items = items[:100]
		next = items[99].UserID.String()
	}
	respond(w, map[string]any{"items": items, "next_after": next})
}
func (s Server) accountMemberChange(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var in struct {
		User    uuid.UUID `json:"user_id"`
		Version int64     `json:"version"`
		Roles   []string  `json:"roles"`
		Active  *bool     `json:"active"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Active == nil {
		http.Error(w, "active is required", 400)
		return
	}
	if err := s.Store.ChangeMemberForPrincipal(r.Context(), p, in.User, in.Version, in.Roles, *in.Active); err != nil {
		s.authorizationError(w, err)
		return
	}
	w.WriteHeader(204)
}
