package handlers

import (
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"time"
)

func (h CaseHandler) Analytics(c *gin.Context) {
	tenant, ok := tenantID(c)
	if !ok {
		return
	}
	from, err := time.Parse(time.RFC3339Nano, c.Query("from"))
	if err != nil {
		presentError(c, casepkg.Invalid("from must be an RFC3339 timestamp"))
		return
	}
	to, err := time.Parse(time.RFC3339Nano, c.Query("to"))
	if err != nil {
		presentError(c, casepkg.Invalid("to must be an RFC3339 timestamp"))
		return
	}
	filter := casepkg.AnalyticsFilter{From: from, To: to}
	if raw := c.Query("inbox_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			presentError(c, casepkg.Invalid("invalid inbox_id"))
			return
		}
		filter.InboxID = &id
	}
	result, err := h.service.CaseAnalytics(c.Request.Context(), tenant, filter)
	if err != nil {
		presentError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(200, result)
}
