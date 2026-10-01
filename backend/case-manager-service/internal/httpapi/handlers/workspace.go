package handlers

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"time"
)

func (h CaseHandler) Session(c *gin.Context) {
	tenant, ok := tenantID(c)
	if !ok {
		return
	}
	p, err := access.Tenant(c.Request.Context(), tenant)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(200, gin.H{"subject": p.Subject, "admin": p.Admin, "tenant_id": tenant})
}
func (h CaseHandler) EvidenceAccess(c *gin.Context) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	resource, ok := pathUUID(c, "resourceId")
	if !ok {
		return
	}
	if err := h.service.EvidenceAccess(c.Request.Context(), tenant, id, c.Param("kind"), resource); err != nil {
		presentError(c, err)
		return
	}
	c.Status(204)
}

func (h CaseHandler) Queue(c *gin.Context) {
	tenant, ok := tenantID(c)
	if !ok {
		return
	}
	f := casepkg.WorkspaceFilters{CaseFilters: casepkg.CaseFilters{Name: c.Query("name"), AssigneeID: c.Query("assignee_id"), IncludeSnoozed: c.Query("include_snoozed") == "true"}, Unassigned: c.Query("unassigned") == "true", ReviewLevel: c.Query("review_level")}
	for _, key := range []string{"include_snoozed", "unassigned", "overdue"} {
		v := c.Query(key)
		if v != "" && v != "true" && v != "false" {
			presentError(c, casepkg.Invalid("invalid boolean filter"))
			return
		}
	}
	f.Overdue = c.Query("overdue") == "true"
	var err error
	f.Before, err = casepkg.ParseCursor(c.Query("cursor"))
	if err != nil {
		presentError(c, err)
		return
	}
	for _, v := range c.QueryArray("status") {
		f.Statuses = append(f.Statuses, casepkg.Status(v))
	}
	for _, v := range c.QueryArray("inbox_id") {
		id, e := uuid.Parse(v)
		if e != nil || id == uuid.Nil {
			presentError(c, casepkg.Invalid("invalid inbox"))
			return
		}
		f.InboxIDs = append(f.InboxIDs, id)
	}
	for key, dest := range map[string]**uuid.UUID{"tag_id": &f.TagID, "related_to": &f.RelatedTo} {
		if v := c.Query(key); v != "" {
			id, e := uuid.Parse(v)
			if e != nil || id == uuid.Nil {
				presentError(c, casepkg.Invalid("invalid "+key))
				return
			}
			*dest = &id
		}
	}
	for key, dest := range map[string]**time.Time{"created_from": &f.CreatedFrom, "created_to": &f.CreatedTo} {
		if v := c.Query(key); v != "" {
			at, e := time.Parse(time.RFC3339Nano, v)
			if e != nil {
				presentError(c, casepkg.Invalid("invalid "+key))
				return
			}
			*dest = &at
		}
	}
	page, err := h.service.WorkspaceQueue(c.Request.Context(), tenant, f, limitQuery(c, 50))
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}
func (h CaseHandler) Overview(c *gin.Context) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	item, err := h.service.WorkspaceOverview(c.Request.Context(), tenant, id)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(200, gin.H{"case": item})
}
func (h CaseHandler) Links(c *gin.Context) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	cursor, err := casepkg.ParseCursor(c.Query("cursor"))
	if err != nil {
		presentError(c, err)
		return
	}
	page, err := h.service.WorkspaceLinks(c.Request.Context(), tenant, id, c.Param("kind"), cursor, limitQuery(c, 50))
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(200, page)
}
func (h CaseHandler) Close(c *gin.Context) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	var body struct {
		Outcome casepkg.Outcome `json:"outcome"`
		Comment string          `json:"comment"`
	}
	if err := bindJSON(c, &body); err != nil {
		presentError(c, err)
		return
	}
	item, err := h.service.CloseCase(c.Request.Context(), tenant, id, body.Outcome, body.Comment)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(200, gin.H{"case": item})
}
