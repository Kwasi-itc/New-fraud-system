package httpapi

import (
	"encoding/json"
	"errors"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"io"
	"net/http"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcemonitor"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerSourceMonitor(router *platformauth.Router, db *pgxpool.Pool, verifier *identity.Verifier, svc service.ScreeningService, ready bool) {
	authorize := manualAuthorization(db, verifier, ready && db != nil)
	monitors := sourcemonitor.Service{DB: db}
	handle := func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		p, err := authorize(c.Request)
		if err != nil {
			manualError(c, err)
			return
		}
		origin, err := uuid.Parse(c.Param("id"))
		if err != nil || origin == uuid.Nil {
			c.JSON(400, gin.H{"error": "Invalid search identifier."})
			return
		}
		if c.Request.Method == http.MethodPost {
			if c.ContentType() != "application/json" {
				c.JSON(415, gin.H{"error": "Expected application/json."})
				return
			}
			var in struct {
				Enabled     *bool  `json:"enabled"`
				RetryTarget string `json:"retry_target,omitempty"`
			}
			dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1024))
			dec.DisallowUnknownFields()
			if dec.Decode(&in) != nil || dec.Decode(new(any)) != io.EOF || (in.Enabled == nil) == (in.RetryTarget == "") {
				c.JSON(400, gin.H{"error": "Specify enabled or a retry target."})
				return
			}
			switch {
			case in.RetryTarget != "":
				target, e := uuid.Parse(in.RetryTarget)
				if e != nil || target == uuid.Nil {
					c.JSON(400, gin.H{"error": "Invalid retry target."})
					return
				}
				err = monitors.Retry(c.Request.Context(), p, origin, target)
			case *in.Enabled:
				_, err = monitors.Enroll(c.Request.Context(), p, origin)
			default:
				err = monitors.SetEnabled(c.Request.Context(), p, origin, false)
			}
			if err != nil {
				manualError(c, err)
				return
			}
		}
		after := uuid.Nil
		if value := c.Query("after"); value != "" {
			var e error
			after, e = uuid.Parse(value)
			if e != nil || after == uuid.Nil {
				c.JSON(400, gin.H{"error": "Invalid history cursor."})
				return
			}
		}
		raw, err := monitors.ViewPage(c.Request.Context(), p, origin, after)
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(200, gin.H{"enabled": false, "enrolled": false})
			return
		}
		if err != nil {
			manualError(c, err)
			return
		}
		var view gin.H
		if json.Unmarshal(raw, &view) != nil {
			c.JSON(503, gin.H{"error": "Monitoring unavailable."})
			return
		}
		view["enrolled"] = true
		if id, ok := view["latest_screening_id"].(string); ok {
			details, e := svc.GetDetails(c.Request.Context(), p.TenantID.String(), id)
			if e != nil {
				manualError(c, e)
				return
			}
			view["latest_result"] = manualView(details)
		}
		c.JSON(200, view)
	}
	router.GET("/v1/manual-screenings/:id/monitor", platformauth.Policy{Capability: "screening:search", PrincipalType: platformauth.PrincipalUser}, handle)
	router.POST("/v1/manual-screenings/:id/monitor", platformauth.Policy{Capability: "screening:search", PrincipalType: platformauth.PrincipalUser}, handle)
}
