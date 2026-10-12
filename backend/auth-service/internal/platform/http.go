package platform

import (
	"context"
	"crypto/rsa"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Server struct {
	Store                   Store
	Provider                *oidcprovider.Provider
	ProxyToken              string
	Key                     *rsa.PrivateKey
	KeyID, Issuer, Audience string
	Logger                  *slog.Logger
	Invitations             *InvitationService
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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		var version int
		if s.Store.DB.QueryRow(ctx, "SELECT max(version) FROM identity.schema_migrations").Scan(&version) != nil || version != SchemaVersion {
			http.Error(w, "not ready", 503)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /internal/login/start", s.start)
	mux.HandleFunc("POST /internal/login/complete", s.complete)
	mux.HandleFunc("POST /internal/session", s.resolve)
	mux.HandleFunc("POST /internal/logout", s.logout)
	mux.HandleFunc("POST /internal/sessions/list", s.sessions)
	mux.HandleFunc("POST /internal/sessions/revoke", s.revokeSessions)
	mux.HandleFunc("POST /internal/members/list", s.members)
	mux.HandleFunc("POST /internal/members/change", s.change)
	mux.HandleFunc("POST /internal/invitations/start", s.invitationStart)
	mux.HandleFunc("POST /internal/invitations/list", s.invitations)
	mux.HandleFunc("POST /internal/invitations/change", s.invitationChange)
	mux.HandleFunc("POST /internal/audit/list", s.auditList)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", "application/json")
		requestID := uuid.NewString()
		w.Header().Set("X-Request-ID", requestID)
		if strings.HasPrefix(r.URL.Path, "/internal/") && (len(s.ProxyToken) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.ProxyToken)) != 1) {
			http.Error(w, "unauthorized", 401)
			return
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
	w.WriteHeader(status)
	respond(w, map[string]string{"error": msg})
}
func (s Server) start(w http.ResponseWriter, r *http.Request) {
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
	l, err := s.Store.ConsumeLogin(r.Context(), in.State, in.Binding, s.Provider.Issuer())
	if err != nil {
		s.fail(w, err)
		return
	}
	id, err := s.Provider.Exchange(r.Context(), in.Code, l.Verifier, l.Nonce)
	if err != nil {
		s.fail(w, ErrDenied)
		return
	}
	var raw string
	if len(l.InvitationDigest) > 0 {
		if s.Invitations == nil {
			s.fail(w, ErrDenied)
			return
		}
		raw, err = s.Invitations.Accept(r.Context(), l.InvitationDigest, id)
	} else {
		raw, err = s.Store.CreateSession(r.Context(), id.Issuer, id.Subject)
	}
	if err != nil {
		s.fail(w, err)
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
	claims := jwt.MapClaims{"iss": s.Issuer, "aud": s.Audience, "sub": v.UserID.String(), "tenant_id": v.TenantID.String(), "roles": v.Roles, "sid": v.ID.String(), "authz_version": v.Version, "iat": now.Unix(), "exp": expires.Unix()}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.KeyID
	signed, err := token.SignedString(s.Key)
	if err != nil {
		s.fail(w, err)
		return
	}
	respond(w, map[string]any{"session": v, "access_token": signed})
}
func (s Server) logout(w http.ResponseWriter, r *http.Request) {
	var in sessionRequest
	if !decode(w, r, &in) {
		return
	}
	if err := s.Store.Logout(r.Context(), in.Session); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s Server) sessions(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Session string    `json:"session"`
		After   uuid.UUID `json:"after"`
	}
	if !decode(w, r, &in) {
		return
	}
	items, err := s.Store.Sessions(r.Context(), in.Session, in.After)
	if err != nil {
		s.fail(w, err)
		return
	}
	next := ""
	if len(items) > 100 {
		items = items[:100]
		next = items[99].ID.String()
	}
	respond(w, map[string]any{"items": items, "next_after": next})
}
func (s Server) revokeSessions(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Session string    `json:"session"`
		Target  uuid.UUID `json:"target"`
		All     bool      `json:"all"`
	}
	if !decode(w, r, &in) {
		return
	}
	if (in.All && in.Target != uuid.Nil) || (!in.All && in.Target == uuid.Nil) {
		http.Error(w, "Choose one session or all sessions", 400)
		return
	}
	current, err := s.Store.RevokeSessions(r.Context(), in.Session, in.Target, in.All)
	if err != nil {
		s.fail(w, err)
		return
	}
	respond(w, map[string]bool{"signed_out": current})
}
func (s Server) members(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Session string    `json:"session"`
		After   uuid.UUID `json:"after"`
	}
	if !decode(w, r, &in) {
		return
	}
	items, err := s.Store.Members(r.Context(), in.Session, in.After)
	if err != nil {
		s.fail(w, err)
		return
	}
	next := ""
	if len(items) > 100 {
		items = items[:100]
		next = items[99].UserID.String()
	}
	respond(w, map[string]any{"items": items, "next_after": next})
}
func (s Server) change(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Session string    `json:"session"`
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
	if err := s.Store.ChangeMember(r.Context(), in.Session, in.User, in.Version, in.Roles, *in.Active); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(204)
}
