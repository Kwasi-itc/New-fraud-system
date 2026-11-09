package mysqlauth

import (
	"encoding/json"
	"net/http"
)

type operation struct {
	Method, Path, Summary string
	Fields                map[string]any
	Required              []string
}

var operations = []operation{
	{"GET", "/healthz", "Process liveness", nil, nil},
	{"GET", "/readyz", "MySQL migrations and dependencies", nil, nil},
	{"POST", "/internal/login/start", "Begin OIDC authorization-code login", nil, nil},
	{"POST", "/internal/login/complete", "Consume login and exchange a provider code", map[string]any{"state": map[string]string{"type": "string"}, "binding": map[string]string{"type": "string"}, "code": map[string]string{"type": "string"}}, []string{"state", "binding", "code"}},
	{"POST", "/internal/session", "Resolve an opaque session and issue a short-lived access JWT", nil, []string{"session"}},
	{"POST", "/internal/logout", "Revoke the supplied session", nil, []string{"session"}},
	{"POST", "/internal/sessions/list", "List this user's active sessions in the session tenant", nil, []string{"session"}},
	{"POST", "/internal/sessions/revoke", "Revoke one owned session or all sessions in this tenant", map[string]any{"target": map[string]string{"type": "string", "format": "uuid"}, "all": map[string]string{"type": "boolean"}}, []string{"session"}},
	{"POST", "/internal/members/list", "List tenant members as a tenant administrator", nil, []string{"session"}},
	{"POST", "/internal/members/change", "Change membership and revoke its sessions", map[string]any{"user_id": map[string]string{"type": "string", "format": "uuid"}, "version": map[string]string{"type": "integer", "format": "int64"}, "roles": map[string]any{"type": "array", "items": map[string]string{"type": "string"}}, "active": map[string]string{"type": "boolean"}}, []string{"session", "user_id", "version", "roles", "active"}},
	{"POST", "/internal/invitations/start", "Begin login using an invitation token", map[string]any{"token": map[string]string{"type": "string"}}, []string{"token"}},
	{"POST", "/internal/invitations/list", "List scoped invitation and delivery status", nil, []string{"session"}},
	{"POST", "/internal/invitations/change", "Create, resend or cancel an invitation", map[string]any{"id": map[string]string{"type": "string", "format": "uuid"}, "action": map[string]any{"type": "string", "enum": []string{"create", "resend", "cancel"}}, "version": map[string]string{"type": "integer", "format": "int64"}, "email": map[string]string{"type": "string"}, "roles": map[string]any{"type": "array", "items": map[string]string{"type": "string"}}}, []string{"session", "id", "action"}},
	{"POST", "/internal/audit/list", "List scoped administration audit history", nil, []string{"session"}},
}

func OpenAPI() map[string]any {
	paths := map[string]any{}
	for _, op := range operations {
		responses := map[string]any{"200": map[string]string{"description": "Successful JSON response"}, "204": map[string]string{"description": "Successful empty response"}, "400": map[string]string{"description": "Invalid request"}, "401": map[string]string{"description": "Missing proxy credential or expired session"}, "403": map[string]string{"description": "Current identity or tenant permission denied"}, "409": map[string]string{"description": "Version, identity or invitation conflict"}, "503": map[string]string{"description": "Storage or provider unavailable"}}
		entry := map[string]any{"summary": op.Summary, "responses": responses}
		if op.Method == "POST" {
			props := map[string]any{}
			for k, v := range op.Fields {
				props[k] = v
			}
			for _, key := range op.Required {
				if key == "session" {
					props[key] = map[string]string{"type": "string", "description": "Opaque auth-issued session; never an actor or tenant ID"}
				}
			}
			if op.Path == "/internal/sessions/list" || op.Path == "/internal/members/list" || op.Path == "/internal/invitations/list" || op.Path == "/internal/audit/list" {
				props["after"] = map[string]string{"type": "string", "format": "uuid"}
			}
			if len(props) > 0 {
				entry["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "additionalProperties": false, "properties": props, "required": op.Required}}}}
			}
			entry["security"] = []map[string][]string{{"proxyBearer": {}}}
		}
		method := "get"
		if op.Method == "POST" {
			method = "post"
		}
		paths[op.Path] = map[string]any{method: entry}
	}
	return map[string]any{"openapi": "3.0.3", "info": map[string]string{"title": "Auth service — MySQL development", "version": "1.0.0", "description": "Internal backend-for-frontend APIs. Authorize with the private proxy bearer token. Most routes also require an opaque session in the body. OIDC identities never grant application roles automatically."}, "servers": []map[string]string{{"url": "http://localhost:8087"}}, "paths": paths, "components": map[string]any{"securitySchemes": map[string]any{"proxyBearer": map[string]string{"type": "http", "scheme": "bearer"}}}}
}
func ConfigureDocs(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(OpenAPI()) })
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!doctype html><html><head><title>Auth MySQL API</title><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css"></head><body><div role="status">Local MySQL development. Internal APIs require a proxy credential and current session permissions.</div><div id="swagger-ui"></div><script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script><script>SwaggerUIBundle({url:'/openapi.json',dom_id:'#swagger-ui',persistAuthorization:false});</script></body></html>`))
	})
	mux.HandleFunc("GET /capabilities", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"profile": "local-development", "database": "mysql", "documented_operations": len(operations), "proxy_authentication": true, "tenant_scope": "current active session membership", "invitation_delivery": "separate worker", "automatic_email_linking": false})
	})
}
