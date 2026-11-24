package platform

import (
	"errors"
	"net/http"
	"strings"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s Server) protected(capability string, handler func(http.ResponseWriter, *http.Request, auth.Principal)) protectedRoute {
	return protectedRoute{policy: auth.Policy{Capability: capability, PrincipalType: auth.PrincipalUser}, handler: func(w http.ResponseWriter, r *http.Request) {
		raw, err := auth.Bearer(r.Header.Get("Authorization"))
		if err != nil || s.Verifier == nil {
			http.Error(w, "authorization_required", 401)
			return
		}
		p, err := s.Verifier.Verify(raw)
		if err != nil {
			http.Error(w, "invalid_or_expired_token", 401)
			return
		}
		tenant, e := uuid.Parse(r.PathValue("tenantId"))
		if e != nil || tenant != p.TenantID || !p.IsHuman() || !p.Can(capability) {
			s.Logger.Warn("protected route denied",
				"method", r.Method,
				"path", r.URL.Path,
				"capability", capability,
				"principal_subject", p.Subject,
				"token_tenant", p.TenantID,
				"path_tenant", tenant,
				"is_human", p.IsHuman(),
				"has_claim_capability", p.Can(capability))
			http.Error(w, "tenant_or_capability_denied", 403)
			return
		}
		if s.Store.DB == nil {
			http.Error(w, "identity_service_unavailable", 503)
			return
		}
		if err := auth.CheckCapability(r.Context(), s.Store.DB, p, capability, false); err != nil {
			s.authorizationError(w, err)
			return
		}
		handler(w, r.WithContext(auth.WithPrincipal(r.Context(), p)), p)
	}}
}

func (s Server) authenticated(handler func(http.ResponseWriter, *http.Request, auth.Principal)) protectedRoute {
	return protectedRoute{
		policy: auth.Policy{PrincipalType: auth.PrincipalUser, AuthenticatedOnly: true},
		handler: func(w http.ResponseWriter, r *http.Request) {
			raw, err := auth.Bearer(r.Header.Get("Authorization"))
			if err != nil || s.Verifier == nil {
				http.Error(w, "authorization_required", 401)
				return
			}
			p, err := s.Verifier.Verify(raw)
			if err != nil {
				http.Error(w, "invalid_or_expired_token", 401)
				return
			}
			if !p.IsHuman() {
				http.Error(w, "human_principal_required", 403)
				return
			}
			if err := s.Store.ValidatePrincipal(r.Context(), p); err != nil {
				s.authorizationError(w, err)
				return
			}
			handler(w, r.WithContext(auth.WithPrincipal(r.Context(), p)), p)
		},
	}
}

func (s Server) authorizationRoutes(mux *routeRegistry) {
	base := "/v1/tenants/{tenantId}"
	mux.HandleProtected("GET "+base+"/client-scopes", s.protected("clients:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		caps := []auth.Capability{}
		for _, cap := range auth.Catalog() {
			if cap.Client != "" {
				caps = append(caps, cap)
			}
		}
		respond(w, map[string]any{"capabilities": caps})
	}))
	mux.HandleProtected("GET "+base+"/capabilities", s.protected("roles:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		respond(w, map[string]any{"capabilities": auth.Catalog()})
	}))
	mux.HandleProtected("GET "+base+"/roles", s.protected("roles:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		out, err := s.Store.Roles(r.Context(), p, r.URL.Query().Get("after"))
		if err != nil {
			s.authorizationError(w, err)
			return
		}
		respond(w, map[string]any{"roles": out})
	}))
	mux.HandleProtected("GET "+base+"/roles/{roleId}", s.protected("roles:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		out, err := s.Store.Role(r.Context(), p, r.PathValue("roleId"))
		if err != nil {
			s.authorizationError(w, err)
			return
		}
		respond(w, out)
	}))
	saveRole := s.protected("roles:manage", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		var in RoleChange
		if !decode(w, r, &in) {
			return
		}
		out, err := s.Store.SaveRole(r.Context(), p, r.PathValue("roleId"), in)
		if err != nil {
			s.authorizationError(w, err)
			return
		}
		respond(w, out)
	})
	mux.HandleProtected("POST "+base+"/roles", saveRole)
	mux.HandleProtected("PUT "+base+"/roles/{roleId}", saveRole)
	mux.HandleProtected("DELETE "+base+"/roles/{roleId}", s.protected("roles:manage", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		var in struct {
			Version int64 `json:"version"`
		}
		if !decode(w, r, &in) {
			return
		}
		if err := s.Store.DeleteRole(r.Context(), p, r.PathValue("roleId"), in.Version); err != nil {
			s.authorizationError(w, err)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleProtected("GET "+base+"/clients", s.protected("clients:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		after, err := optionalUUID(r.URL.Query().Get("after"))
		if err != nil {
			http.Error(w, "invalid cursor", 400)
			return
		}
		out, err := s.Store.FilteredClients(r.Context(), p, after, r.URL.Query().Get("query"))
		if err != nil {
			s.authorizationError(w, err)
			return
		}
		respond(w, map[string]any{"clients": out})
	}))
	mux.HandleProtected("GET "+base+"/clients/{clientId}", s.protected("clients:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		id, err := uuid.Parse(r.PathValue("clientId"))
		if err != nil {
			s.authorizationError(w, auth.Invalid("Invalid client."))
			return
		}
		out, err := s.Store.Client(r.Context(), p, id)
		if err != nil {
			s.authorizationError(w, err)
			return
		}
		respond(w, out)
	}))
	saveClient := s.protected("clients:manage", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		var in ClientChange
		if !decode(w, r, &in) {
			return
		}
		id, err := optionalUUID(r.PathValue("clientId"))
		if err != nil {
			http.Error(w, "invalid client", 400)
			return
		}
		out, err := s.Store.SaveClient(r.Context(), p, id, in)
		if err != nil {
			s.authorizationError(w, err)
			return
		}
		respond(w, out)
	})
	mux.HandleProtected("POST "+base+"/clients", saveClient)
	mux.HandleProtected("PUT "+base+"/clients/{clientId}", saveClient)
	mux.HandleProtected("GET "+base+"/client-keys", s.protected("clients:manage", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		id, err := uuid.Parse(r.URL.Query().Get("client_id"))
		if err != nil {
			http.Error(w, "invalid client", 400)
			return
		}
		out, err := s.Store.ClientKeys(r.Context(), p, id, r.URL.Query().Get("after"))
		if err != nil {
			s.authorizationError(w, err)
			return
		}
		respond(w, map[string]any{"keys": out})
	}))
	mux.HandleProtected("POST "+base+"/client-keys", s.protected("clients:manage", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		var in ClientKey
		if !decode(w, r, &in) {
			return
		}
		if err := s.Store.RegisterClientKey(r.Context(), p, in); err != nil {
			s.authorizationError(w, err)
			return
		}
		w.WriteHeader(201)
	}))
	mux.HandleProtected("DELETE "+base+"/client-keys/{kid}", s.protected("clients:manage", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		id, err := uuid.Parse(r.URL.Query().Get("client_id"))
		if err != nil {
			http.Error(w, "invalid client", 400)
			return
		}
		if err = s.Store.RevokeClientKey(r.Context(), p, id, r.PathValue("kid")); err != nil {
			s.authorizationError(w, err)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleProtected("GET "+base+"/clients/{clientId}/scope-approvals", s.protected("clients:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		id, err := uuid.Parse(r.PathValue("clientId"))
		if err != nil {
			http.Error(w, "invalid client", 400)
			return
		}
		out, err := s.Store.ScopeApprovals(r.Context(), p, id)
		if err != nil {
			s.authorizationError(w, err)
			return
		}
		respond(w, map[string]any{"approvals": out})
	}))
	mux.HandleProtected("POST "+base+"/clients/{clientId}/scope-approvals/{scope}", s.protected("clients:approve-sensitive", func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		id, err := uuid.Parse(r.PathValue("clientId"))
		if err != nil {
			http.Error(w, "invalid client", 400)
			return
		}
		if err = s.Store.ApproveScope(r.Context(), p, id, r.PathValue("scope")); err != nil {
			s.authorizationError(w, err)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandlePublic("POST /oauth/token", s.oauthToken)
}
func optionalUUID(raw string) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, nil
	}
	return uuid.Parse(raw)
}
func (s Server) authorizationError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrDenied) {
		w.WriteHeader(http.StatusForbidden)
		respond(w, map[string]string{"error": "permission_denied"})
		return
	}
	if errors.Is(err, ErrConflict) {
		w.WriteHeader(http.StatusConflict)
		respond(w, map[string]string{"error": "version_or_reference_conflict"})
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "23503" || pg.Code == "23505") {
		w.WriteHeader(http.StatusConflict)
		respond(w, map[string]string{"error": "resource_conflict"})
		return
	}
	status, message := auth.HTTPError(err)
	if status == 500 {
		s.Logger.Error("authorization operation failed", "error", err)
		status = 503
	}
	http.Error(w, message, status)
}
func (s Server) oauthToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		w.WriteHeader(415)
		respond(w, map[string]string{"error": "invalid_request"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(400)
		respond(w, map[string]string{"error": "invalid_request"})
		return
	}
	// OAuth parameters must be in the POST body and unambiguous.
	for key, values := range r.PostForm {
		if len(values) != 1 || !strings.Contains("|grant_type|client_id|client_assertion_type|client_assertion|audience|scope|", "|"+key+"|") {
			w.WriteHeader(400)
			respond(w, map[string]string{"error": "invalid_request"})
			return
		}
	}
	if len(r.URL.RawQuery) > 0 || r.PostForm.Get("grant_type") != "client_credentials" || r.PostForm.Get("client_assertion_type") != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
		w.WriteHeader(400)
		respond(w, map[string]string{"error": "unsupported_grant_type"})
		return
	}
	id, err := uuid.Parse(r.PostForm.Get("client_id"))
	if err != nil {
		w.WriteHeader(401)
		respond(w, map[string]string{"error": "invalid_client"})
		return
	}
	token, err := s.ExchangeClientProof(r.Context(), id, r.PostForm.Get("client_assertion"), r.PostForm.Get("audience"), strings.Fields(r.PostForm.Get("scope")))
	if err != nil {
		var e *auth.Error
		if errors.As(err, &e) && e.Code == 400 {
			w.WriteHeader(400)
			respond(w, map[string]string{"error": "invalid_scope"})
			return
		}
		if errors.Is(err, ErrDenied) {
			w.WriteHeader(401)
			respond(w, map[string]string{"error": "invalid_client"})
			return
		}
		s.Logger.Error("client token exchange failed", "error", err)
		w.WriteHeader(503)
		respond(w, map[string]string{"error": "temporarily_unavailable"})
		return
	}
	respond(w, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": int(auth.ClientTTL.Seconds())})
}
