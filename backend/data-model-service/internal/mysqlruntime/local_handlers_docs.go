package mysqlruntime

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

func registerLocalDocs(r *gin.Engine) error {
	var spec gin.H
	if err := yaml.Unmarshal(httpapi.OpenAPISpec(), &spec); err != nil {
		return err
	}
	r.GET("/docs", func(c *gin.Context) { c.Data(200, "text/html; charset=utf-8", []byte(localDocs)) })
	r.GET("/openapi.json", func(c *gin.Context) { c.JSON(200, spec) })
	r.GET("/openapi.yaml", func(c *gin.Context) { c.Data(200, "application/yaml; charset=utf-8", httpapi.OpenAPISpec()) })
	return nil
}

const localDocs = `<!doctype html><html><head><title>Data Model MySQL API</title><link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"></head><body><p style="padding:16px">Complete Data Model API reference. Local MySQL mode has login disabled. All endpoints are documented here. Tenant provisioning, table creation, native field creation, presentation edits and metadata reads are wired to MySQL. Deletion, enums, constraint edits and caption changes remain incomplete; see MYSQL_ENDPOINT_STATUS.md in the service directory. <a href="/openapi.yaml">Full OpenAPI YAML</a></p><div id="swagger-ui"></div><script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script><script>SwaggerUIBundle({url:'/openapi.json',dom_id:'#swagger-ui',validatorUrl:null,deepLinking:true});</script></body></html>`
