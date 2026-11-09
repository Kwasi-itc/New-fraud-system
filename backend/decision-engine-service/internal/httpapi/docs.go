package httpapi

import (
	"embed"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed openapi.yaml
var openAPISpec []byte

func registerDocsRoutes(router *gin.Engine) {
	registerDocsHTML(router, docsHTML)
}

func registerDocsHTML(router *gin.Engine, swagger string) {
	router.GET("/openapi.yaml", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/yaml; charset=utf-8", openAPISpec)
	})

	router.GET("/docs", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swagger))
	})

	router.GET("/redoc", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(redocHTML))
	})
}

// RegisterLocalDocs labels the shared API reference with the active local scope.
func RegisterLocalDocs(router *gin.Engine) {
	local := strings.Replace(docsHTML, "This service supports scenario authoring, decision evaluation, test runs, workflows, screening,\n        scoring, and execution tracking.", "Local MySQL development profile: all documented routes, durable workflows, exact-name watchlist screening,\n        deterministic additional scoring, callback and event inboxes. Delivery uses private local receivers.\n        Expression assistance is deterministic and uses no AI provider. Check <a href=\"/capabilities\">/capabilities</a> for local destination URLs and supported scoring ruleset.", 1)
	registerDocsHTML(router, local)
}

var _ embed.FS

const docsHTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Decision Engine Service Swagger UI</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
    <style>
      html { box-sizing: border-box; overflow-y: scroll; }
      *, *:before, *:after { box-sizing: inherit; }
      body { margin: 0; background: #f3f4f6; }
      header { padding: 16px 24px; background: #111827; color: #f9fafb; font-family: system-ui, sans-serif; }
      header a { color: #93c5fd; text-decoration: none; }
      #swagger-ui { max-width: 1200px; margin: 0 auto; }
    </style>
  </head>
  <body>
    <header>
      <strong>Decision Engine Service API</strong>
      <span style="margin-left:16px;">Raw spec: <a href="/openapi.yaml">/openapi.yaml</a></span>
      <div style="margin-top:8px;font-size:14px;line-height:1.5;color:#d1d5db;">
        This service supports scenario authoring, decision evaluation, test runs, workflows, screening,
        scoring, and execution tracking.
      </div>
    </header>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
      window.onload = () => {
        window.ui = SwaggerUIBundle({
          url: '/openapi.yaml',
          dom_id: '#swagger-ui',
          deepLinking: true,
          docExpansion: 'list',
          defaultModelsExpandDepth: 2
        });
      };
    </script>
  </body>
</html>`

const redocHTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Decision Engine Service Redoc</title>
    <style>
      body { margin: 0; background: #ffffff; }
      header { padding: 16px 24px; background: #111827; color: #f9fafb; font-family: system-ui, sans-serif; }
      header a { color: #93c5fd; text-decoration: none; }
    </style>
  </head>
  <body>
    <header>
      <strong>Decision Engine Service API</strong>
      <span style="margin-left:16px;">Raw spec: <a href="/openapi.yaml">/openapi.yaml</a></span>
      <span style="margin-left:16px;">Swagger UI: <a href="/docs">/docs</a></span>
    </header>
    <redoc spec-url="/openapi.yaml"></redoc>
    <script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
  </body>
</html>`
