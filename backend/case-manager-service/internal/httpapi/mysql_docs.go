package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// MountMySQLDocs derives path/method coverage from the actual registered router.
// It documents source placeholders and unavailable owner contracts explicitly.
func MountMySQLDocs(router *gin.Engine) {
	paths := map[string]any{}
	examples := map[string]any{
		"/v1/tenants/:tenantId/inboxes":                gin.H{"name": "High risk transfers"},
		"/v1/tenants/:tenantId/cases":                  gin.H{"inbox_id": "00000000-0000-0000-0000-000000000001", "name": "Investigate a high amount transfer", "type": "decision", "decision_ids": []string{}},
		"/v1/tenants/:tenantId/tags":                   gin.H{"target": "case", "name": "High amount", "color": "red"},
		"/v1/tenants/:tenantId/cases/:caseId/comments": gin.H{"comment": "Review the source of funds and recent activity."},
	}
	for _, route := range router.Routes() {
		path := route.Path
		params := []any{}
		parts := strings.Split(path, "/")
		for n, part := range parts {
			if strings.HasPrefix(part, ":") {
				name := strings.TrimPrefix(part, ":")
				parts[n] = "{" + name + "}"
				params = append(params, gin.H{"name": name, "in": "path", "required": true, "schema": gin.H{"type": "string"}})
			}
		}
		path = strings.Join(parts, "/")
		public := path == "/healthz" || path == "/readyz"
		scheme := "InvestigatorSession"
		if strings.HasPrefix(path, "/internal/") || strings.HasPrefix(path, "/v1/screening-events/") {
			scheme = "ServiceCredential"
		}
		op := gin.H{"summary": route.Method + " " + path, "parameters": params, "responses": gin.H{"200": gin.H{"description": "Successful operation; mutation responses may use 201 or 204"}, "400": gin.H{"description": "Invalid request"}, "401": gin.H{"description": "Missing or invalid credential/session"}, "403": gin.H{"description": "Tenant or inbox access denied"}, "404": gin.H{"description": "Resource not found"}, "409": gin.H{"description": "Conflicting state or assignment capacity"}, "503": gin.H{"description": "Required owner contract unavailable"}}}
		if !public {
			op["security"] = []any{gin.H{scheme: []string{}}}
		}
		if route.Method == http.MethodPost || route.Method == http.MethodPut || route.Method == http.MethodPatch {
			schema := gin.H{"type": "object"}
			media := gin.H{"schema": schema}
			if example, ok := examples[route.Path]; ok {
				media["example"] = example
			}
			op["requestBody"] = gin.H{"description": "Use the endpoint contract in MYSQL_DOCKER.md; path IDs must reference existing resources.", "content": gin.H{"application/json": media}}
		}
		if strings.Contains(path, "/ai-case-reviews/") || path == "/internal/v1/auto-assignment/run" {
			op["description"] = "Existing source placeholder: returns 501. Automatic assignment runs in the maintenance worker."
			op["responses"].(gin.H)["501"] = gin.H{"description": "Source feature not implemented"}
		}
		item, ok := paths[path].(gin.H)
		if !ok {
			item = gin.H{}
		}
		item[strings.ToLower(route.Method)] = op
		paths[path] = item
	}
	spec := gin.H{"openapi": "3.0.3", "info": gin.H{"title": "Case management — local MySQL", "version": "1.0.0", "description": "Uses the shared fraud database and current auth sessions. Manual/continuous intake, review and evidence callbacks use authoritative screening-owner contracts. Evidence imports verify source bytes and stable delivery receipts. Existing AI review and internal auto-assignment placeholders return 501."}, "servers": []any{gin.H{"url": "http://localhost:8086"}}, "paths": paths, "components": gin.H{"securitySchemes": gin.H{"InvestigatorSession": gin.H{"type": "http", "scheme": "bearer", "bearerFormat": "JWT", "description": "Use the access token from the auth service; active session, matching tenant and case roles are required."}, "ServiceCredential": gin.H{"type": "http", "scheme": "bearer", "description": "Local case integration credential; restricted to configured service tenants."}}}}
	router.GET("/openapi.json", func(c *gin.Context) { c.JSON(200, spec) })
	router.GET("/docs", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(200, `<!doctype html><html><head><title>Case management MySQL API</title><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css"></head><body><div id="swagger-ui"></div><script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script><script>SwaggerUIBundle({url:'/openapi.json',dom_id:'#swagger-ui',persistAuthorization:false})</script></body></html>`)
	})
}
