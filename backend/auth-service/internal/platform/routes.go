package platform

import (
	"net/http"
	"slices"
	"strings"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

// The auth API uses net/http rather than Gin. Keep its underlying mux private:
// every registration must explicitly be public, BFF bootstrap, or human-capability
// protected. A protected handler carries the same policy it enforces.
type routeRegistry struct {
	mux      *http.ServeMux
	policies []auth.RoutePolicy
}

type protectedRoute struct {
	policy  auth.Policy
	handler http.HandlerFunc
}

func newRouteRegistry() *routeRegistry { return &routeRegistry{mux: http.NewServeMux()} }

func (r *routeRegistry) register(pattern string, policy auth.Policy, handler http.HandlerFunc) {
	if err := policy.Validate(); err != nil {
		panic(err)
	}
	method, path, ok := strings.Cut(pattern, " ")
	if !ok || method == "" || !strings.HasPrefix(path, "/") {
		panic("explicit HTTP method and route are required")
	}
	r.mux.HandleFunc(pattern, handler)
	r.policies = append(r.policies, auth.RoutePolicy{Method: method, Path: path, Policy: policy})
}
func (r *routeRegistry) HandlePublic(pattern string, handler http.HandlerFunc) {
	r.register(pattern, auth.Policy{Public: true}, handler)
}
func (r *routeRegistry) HandleBootstrap(pattern string, handler http.HandlerFunc) {
	if !strings.Contains(pattern, " /internal/") {
		panic("bootstrap routes must use the internal auth namespace")
	}
	r.register(pattern, auth.Policy{Capability: "auth:bootstrap", PrincipalType: auth.PrincipalService, ControlPlane: true}, handler)
}
func (r *routeRegistry) HandleProtected(pattern string, route protectedRoute) {
	r.register(pattern, route.policy, route.handler)
}
func (r *routeRegistry) Inventory() []auth.RoutePolicy {
	values := append([]auth.RoutePolicy(nil), r.policies...)
	slices.SortFunc(values, func(a, b auth.RoutePolicy) int {
		if a.Path != b.Path {
			return strings.Compare(a.Path, b.Path)
		}
		return strings.Compare(a.Method, b.Method)
	})
	return values
}
func (r *routeRegistry) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	r.mux.ServeHTTP(w, request)
}
