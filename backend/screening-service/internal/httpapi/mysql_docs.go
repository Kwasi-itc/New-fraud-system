package httpapi

import (
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
	"net/http"
	"strings"
)

func mountMySQLDocs(router *gin.Engine) {
	var spec map[string]any
	if e := yaml.Unmarshal(openAPISpec, &spec); e != nil {
		panic("invalid embedded screening OpenAPI document")
	}
	spec["servers"] = []any{gin.H{"url": "http://localhost:8085"}}
	info := spec["info"].(map[string]any)
	info["title"] = "Screening service - local MySQL"
	info["description"] = "Native shared MySQL runtime. The default provider searches a synthetic development catalog only. Owner routes require private operator and service credentials; human routes require a current auth access JWT."
	paths := spec["paths"].(map[string]any)
	for _, route := range router.Routes() {
		parts := strings.Split(route.Path, "/")
		parameters := []any{}
		for i, part := range parts {
			if strings.HasPrefix(part, ":") {
				name := strings.TrimPrefix(part, ":")
				parts[i] = "{" + name + "}"
				parameters = append(parameters, gin.H{"name": name, "in": "path", "required": true, "schema": gin.H{"type": "string"}})
			}
		}
		path := strings.Join(parts, "/")
		method := strings.ToLower(route.Method)
		item, ok := paths[path].(map[string]any)
		if !ok {
			item = map[string]any{}
		}
		if _, ok = item[method]; !ok {
			item[method] = gin.H{"summary": route.Method + " " + path, "parameters": parameters, "responses": gin.H{"200": gin.H{"description": "Successful operation"}, "401": gin.H{"description": "Authentication required"}, "403": gin.H{"description": "Tenant or resource access denied"}, "409": gin.H{"description": "Conflicting state"}}}
		}
		paths[path] = item
	}
	router.GET("/openapi.json", func(c *gin.Context) { c.JSON(http.StatusOK, spec) })
	router.GET("/openapi.yaml", func(c *gin.Context) {
		b, e := yaml.Marshal(spec)
		if e != nil {
			c.Status(500)
			return
		}
		c.Data(200, "application/yaml; charset=utf-8", b)
	})
	router.GET("/docs", func(c *gin.Context) {
		c.Data(200, "text/html; charset=utf-8", []byte(strings.ReplaceAll(docsHTML, "/openapi.yaml", "/openapi.json")))
	})
	router.GET("/redoc", func(c *gin.Context) {
		c.Data(200, "text/html; charset=utf-8", []byte(strings.ReplaceAll(redocHTML, "/openapi.yaml", "/openapi.json")))
	})
}
