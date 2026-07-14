package httpapi

import (
	"embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed openapi.yaml
var openAPISpec []byte

func registerDocsRoutes(router *gin.Engine) {
	router.GET("/openapi.yaml", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/yaml; charset=utf-8", openAPISpec)
	})

	router.GET("/docs", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(docsHTML))
	})

	router.GET("/redoc", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(redocHTML))
	})
}

var _ embed.FS

const docsHTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Ingestion Service Swagger UI</title>
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
      <strong>Ingestion Service API</strong>
      <span style="margin-left:16px;">Raw spec: <a href="/openapi.yaml">/openapi.yaml</a></span>
      <div style="margin-top:8px;font-size:14px;line-height:1.5;color:#d1d5db;">
        Phase 0 and Phase 1 scaffold. Health and readiness routes are live. Ingestion endpoints will be added as implementation moves into synchronous and batch writes.
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
    <title>Ingestion Service Redoc</title>
    <style>
      body { margin: 0; background: #ffffff; }
      header { padding: 16px 24px; background: #111827; color: #f9fafb; font-family: system-ui, sans-serif; }
      header a { color: #93c5fd; text-decoration: none; }
    </style>
  </head>
  <body>
    <header>
      <strong>Ingestion Service API</strong>
      <span style="margin-left:16px;">Raw spec: <a href="/openapi.yaml">/openapi.yaml</a></span>
      <span style="margin-left:16px;">Swagger UI: <a href="/docs">/docs</a></span>
    </header>
    <redoc spec-url="/openapi.yaml"></redoc>
    <script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
  </body>
</html>`
