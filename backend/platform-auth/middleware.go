package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TokenVerifier interface {
	Verify(string) (Principal, error)
}

func Bearer(header string) (string, error) {
	fields := strings.Fields(header)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return "", errors.New("bearer token required")
	}
	return fields[1], nil
}
func PrincipalFromContext(c *gin.Context) *Principal {
	p, ok := FromContext(c.Request.Context())
	if !ok {
		return nil
	}
	return &p
}
func TenantFromContext(c *gin.Context) uuid.UUID {
	if p := PrincipalFromContext(c); p != nil {
		return p.TenantID
	}
	return uuid.Nil
}
func Authenticate(v TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := Bearer(c.GetHeader("Authorization"))
		if err != nil || v == nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "authorization_required"})
			return
		}
		p, err := v.Verify(raw)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid_or_expired_token"})
			return
		}
		c.Request = c.Request.WithContext(WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
}
func RequireCapability(capability string) gin.HandlerFunc {
	if _, ok := LookupCapability(capability); !ok {
		panic("unregistered capability: " + capability)
	}
	return func(c *gin.Context) {
		p := PrincipalFromContext(c)
		if p == nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "authorization_required"})
			return
		}
		if param := c.Param("tenantId"); param != "" {
			id, err := uuid.Parse(param)
			if err != nil || id == uuid.Nil || id != p.TenantID {
				c.AbortWithStatusJSON(403, gin.H{"error": "tenant_access_denied"})
				return
			}
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), requiredCapabilityKey{}, capability))
		if !p.Can(capability) {
			c.AbortWithStatusJSON(403, gin.H{"error": "insufficient_permissions"})
			return
		}
		c.Next()
	}
}
func RequireHuman() gin.HandlerFunc   { return requireType(PrincipalUser) }
func RequireService() gin.HandlerFunc { return requireType(PrincipalService) }
func requireType(t PrincipalType) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := PrincipalFromContext(c)
		if p == nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "authorization_required"})
			return
		}
		if p.Type != t {
			c.AbortWithStatusJSON(403, gin.H{"error": string(t) + "_principal_required"})
			return
		}
		c.Next()
	}
}

type Policy struct {
	BodyTenant        bool          `json:"body_tenant,omitempty"`
	Capability        string        `json:"capability"`
	PrincipalType     PrincipalType `json:"principal_type,omitempty"`
	Public            bool          `json:"public,omitempty"`
	AuthenticatedOnly bool          `json:"authenticated_only,omitempty"`
	// ControlPlane permits a tenantless service principal only on dedicated operations.
	ControlPlane bool `json:"control_plane,omitempty"`
}

func (p Policy) Validate() error {
	if p.Public {
		if p.Capability != "" || p.PrincipalType != "" || p.ControlPlane || p.BodyTenant || p.AuthenticatedOnly {
			return errors.New("public route cannot also carry a protected policy")
		}
		return nil
	}
	if p.AuthenticatedOnly {
		if p.Capability != "" || p.PrincipalType != PrincipalUser || p.ControlPlane || p.BodyTenant {
			return errors.New("authenticated-only routes must require a human principal without a capability")
		}
		return nil
	}
	c, ok := LookupCapability(p.Capability)
	if !ok {
		return errors.New("route requires a registered capability")
	}
	if p.PrincipalType != "" && !validPrincipalType(p.PrincipalType) {
		return errors.New("unknown principal restriction")
	}
	if p.ControlPlane && (p.PrincipalType != PrincipalService || !IsControlPlaneCapability(p.Capability)) {
		return errors.New("invalid control-plane policy")
	}
	if p.PrincipalType == PrincipalUser && !c.Human || p.PrincipalType == PrincipalService && !c.Service || p.PrincipalType == PrincipalClient && c.Client == "" {
		return errors.New("principal restriction conflicts with capability")
	}
	return nil
}
func (p Policy) Middleware(v TokenVerifier) []gin.HandlerFunc {
	if err := p.Validate(); err != nil {
		panic(err)
	}
	if p.Public {
		return nil
	}
	if p.AuthenticatedOnly {
		return []gin.HandlerFunc{Authenticate(v), RequireHuman()}
	}
	handlers := []gin.HandlerFunc{Authenticate(v), RequireCapability(p.Capability), func(c *gin.Context) {
		a := PrincipalFromContext(c)
		if !p.ControlPlane && a.TenantID == uuid.Nil {
			c.AbortWithStatusJSON(403, gin.H{"error": "tenant_required"})
			return
		}
		c.Next()
	}}
	if p.BodyTenant {
		handlers = append(handlers, RequireBodyTenant())
	}
	if p.PrincipalType != "" {
		handlers = append(handlers, requireType(p.PrincipalType))
	}
	return handlers
}

// Router requires an explicit policy at registration. ValidateCoverage rejects
// routes registered directly on the engine without a declared policy.
type Router struct {
	engine        *gin.Engine
	verifier      TokenVerifier
	policies      map[string]Policy
	authenticated []gin.HandlerFunc
}

func (r *Router) UseAuthenticated(h ...gin.HandlerFunc) {
	r.authenticated = append(r.authenticated, h...)
}

// RouteGroup shares the engine-wide policy registry. Its middleware is applied
// after authentication and capability checks on each explicitly declared route.
type RouteGroup struct {
	router   *Router
	prefix   string
	handlers []gin.HandlerFunc
}

func (r *Router) Use(h ...gin.HandlerFunc) { r.engine.Use(h...) }
func (r *Router) Group(prefix string, h ...gin.HandlerFunc) *RouteGroup {
	return &RouteGroup{router: r, prefix: strings.TrimSuffix(prefix, "/"), handlers: h}
}
func (g *RouteGroup) Group(prefix string, h ...gin.HandlerFunc) *RouteGroup {
	return &RouteGroup{router: g.router, prefix: g.prefix + strings.TrimSuffix(prefix, "/"), handlers: append(append([]gin.HandlerFunc{}, g.handlers...), h...)}
}
func (g *RouteGroup) Use(h ...gin.HandlerFunc) { g.handlers = append(g.handlers, h...) }
func (g *RouteGroup) Handle(method, path string, p Policy, h ...gin.HandlerFunc) {
	g.router.Handle(method, g.prefix+path, p, append(append([]gin.HandlerFunc{}, g.handlers...), h...)...)
}
func (g *RouteGroup) GET(path string, p Policy, h ...gin.HandlerFunc) {
	g.Handle(http.MethodGet, path, p, h...)
}
func (g *RouteGroup) POST(path string, p Policy, h ...gin.HandlerFunc) {
	g.Handle(http.MethodPost, path, p, h...)
}
func (g *RouteGroup) PUT(path string, p Policy, h ...gin.HandlerFunc) {
	g.Handle(http.MethodPut, path, p, h...)
}
func (g *RouteGroup) PATCH(path string, p Policy, h ...gin.HandlerFunc) {
	g.Handle(http.MethodPatch, path, p, h...)
}
func (g *RouteGroup) DELETE(path string, p Policy, h ...gin.HandlerFunc) {
	g.Handle(http.MethodDelete, path, p, h...)
}

func NewRouter(engine *gin.Engine, v TokenVerifier) *Router {
	return &Router{engine: engine, verifier: v, policies: map[string]Policy{}}
}
func (r *Router) Handle(method, path string, p Policy, h ...gin.HandlerFunc) {
	key := method + " " + path
	if _, ok := r.policies[key]; ok {
		panic("duplicate route policy: " + key)
	}
	if err := p.Validate(); err != nil {
		panic(fmt.Errorf("%s: %w", key, err))
	}
	r.policies[key] = p
	pipeline := p.Middleware(r.verifier)
	if !p.Public {
		pipeline = append(pipeline, r.authenticated...)
	}
	r.engine.Handle(method, path, append(pipeline, h...)...)
}
func (r *Router) GET(path string, p Policy, h ...gin.HandlerFunc) {
	r.Handle(http.MethodGet, path, p, h...)
}
func (r *Router) POST(path string, p Policy, h ...gin.HandlerFunc) {
	r.Handle(http.MethodPost, path, p, h...)
}
func (r *Router) PUT(path string, p Policy, h ...gin.HandlerFunc) {
	r.Handle(http.MethodPut, path, p, h...)
}
func (r *Router) PATCH(path string, p Policy, h ...gin.HandlerFunc) {
	r.Handle(http.MethodPatch, path, p, h...)
}
func (r *Router) DELETE(path string, p Policy, h ...gin.HandlerFunc) {
	r.Handle(http.MethodDelete, path, p, h...)
}
func (r *Router) ValidateCoverage() error {
	for _, route := range r.engine.Routes() {
		if _, ok := r.policies[route.Method+" "+route.Path]; !ok {
			return fmt.Errorf("route has no policy: %s %s", route.Method, route.Path)
		}
	}
	return nil
}

func IsControlPlaneCapability(capability string) bool {
	return capability == "auth:bootstrap" || capability == "tenants:provision" || capability == "platform:reconcile"
}

// RequireBodyTenant binds callback payloads to the verified tenant before domain work.
func RequireBodyTenant() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := PrincipalFromContext(c)
		if p == nil {
			c.AbortWithStatus(401)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
		if err != nil {
			c.AbortWithStatusJSON(413, gin.H{"error": "request_too_large"})
			return
		}
		var envelope struct {
			TenantID uuid.UUID `json:"tenant_id"`
		}
		if json.Unmarshal(body, &envelope) != nil || envelope.TenantID == uuid.Nil {
			c.AbortWithStatusJSON(400, gin.H{"error": "tenant_id_required"})
			return
		}
		if envelope.TenantID != p.TenantID {
			c.AbortWithStatusJSON(403, gin.H{"error": "tenant_access_denied"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Next()
	}
}

// RoutePolicy is a value snapshot for generated documentation and contract tests.
type RoutePolicy struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Policy
}

func (r *Router) Inventory() []RoutePolicy {
	out := make([]RoutePolicy, 0, len(r.policies))
	for key, policy := range r.policies {
		method, path, _ := strings.Cut(key, " ")
		out = append(out, RoutePolicy{method, path, policy})
	}
	slices.SortFunc(out, func(a, b RoutePolicy) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return strings.Compare(a.Method, b.Method)
	})
	return out
}
