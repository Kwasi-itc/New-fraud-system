package mysqlruntime

import (
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// localAuthorClient forwards only requests mounted by the public router.
// Caller identity headers are never forwarded to the private worker.
type localAuthorClient struct {
	baseURL string
	client  *http.Client
	secret  func() ([]byte, error)
}

func newLocalAuthorClient() *localAuthorClient {
	return &localAuthorClient{
		baseURL: "http://data-model-author:8081",
		client:  &http.Client{Timeout: 65 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		secret:  func() ([]byte, error) { return os.ReadFile("/run/secrets/data_model_mysql_password") },
	}
}
func (p *localAuthorClient) Forward(c *gin.Context) {
	// Mounted routes contain one resource UUID, except the admin report.
	if c.FullPath() != "/v1/admin/reconcile" {
		if len(c.Params) != 1 {
			c.JSON(400, gin.H{"error": "invalid_tenant"})
			return
		}
		id, err := uuid.Parse(c.Params[0].Value)
		if err != nil || id == uuid.Nil {
			c.JSON(400, gin.H{"error": "invalid_tenant"})
			return
		}
	}
	secret, err := p.secret()
	if err != nil {
		c.JSON(503, gin.H{"error": "authoring_unavailable"})
		return
	}
	request, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, p.baseURL+c.Request.URL.RequestURI(), c.Request.Body)
	if err != nil {
		c.JSON(503, gin.H{"error": "authoring_unavailable"})
		return
	}
	request.Header.Set("Authorization", "Bearer "+string(secret))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", c.GetHeader("Idempotency-Key"))
	response, err := p.client.Do(request)
	if err != nil {
		c.JSON(503, gin.H{"error": "authoring_unavailable", "message": "Retry the identical request with the same Idempotency-Key."})
		return
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		c.JSON(503, gin.H{"error": "authoring_unavailable"})
		return
	}
	c.Header("Idempotency-Key", response.Header.Get("Idempotency-Key"))
	c.Header("Retry-After", response.Header.Get("Retry-After"))
	c.Data(response.StatusCode, "application/json", body)
}
