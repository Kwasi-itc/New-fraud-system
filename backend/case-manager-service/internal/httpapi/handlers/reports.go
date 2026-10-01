package handlers

import (
	"encoding/json"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"strings"
)

func (h CaseHandler) ListReports(c *gin.Context) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	before, err := casepkg.ParseCursor(c.Query("cursor"))
	if err != nil {
		presentError(c, err)
		return
	}
	result, err := h.service.ListReports(c.Request.Context(), tenant, id, before, limitQuery(c, 20))
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(200, result)
}
func (h CaseHandler) GetReport(c *gin.Context) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	report, ok := pathUUID(c, "reportId")
	if !ok {
		return
	}
	result, err := h.service.GetReport(c.Request.Context(), tenant, id, report)
	if err != nil {
		presentError(c, err)
		return
	}
	if !strings.HasSuffix(c.FullPath(), "/export") {
		c.JSON(200, gin.H{"report": result})
		return
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		presentError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", `attachment; filename="report-`+report.String()+`.json"`)
	c.Data(200, "application/json", data)
}
func (h CaseHandler) CreateReport(c *gin.Context) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	var body struct {
		ID      uuid.UUID             `json:"id"`
		Content casepkg.ReportContent `json:"content"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if err := bindJSON(c, &body); err != nil {
		presentError(c, err)
		return
	}
	result, err := h.service.CreateReport(c.Request.Context(), tenant, id, body.ID, body.Content)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(201, gin.H{"report": result})
}
func (h CaseHandler) UpdateReport(c *gin.Context)   { h.changeReport(c, false) }
func (h CaseHandler) CompleteReport(c *gin.Context) { h.changeReport(c, true) }
func (h CaseHandler) changeReport(c *gin.Context, complete bool) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	report, ok := pathUUID(c, "reportId")
	if !ok {
		return
	}
	var body struct {
		Version int                    `json:"version"`
		Content *casepkg.ReportContent `json:"content"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if err := bindJSON(c, &body); err != nil {
		presentError(c, err)
		return
	}
	if complete && body.Content != nil {
		presentError(c, casepkg.Invalid("save the draft before completing it"))
		return
	}
	result, err := h.service.ChangeReport(c.Request.Context(), tenant, id, report, body.Version, body.Content, complete)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(200, gin.H{"report": result})
}
