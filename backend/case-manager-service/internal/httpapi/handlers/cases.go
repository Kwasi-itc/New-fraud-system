package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
)

type CaseHandler struct {
	service service.CaseService
}

func NewCaseHandler(service service.CaseService) CaseHandler {
	return CaseHandler{service: service}
}

func (h CaseHandler) List(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	filters := casepkg.CaseFilters{
		Name:           c.Query("name"),
		IncludeSnoozed: c.Query("include_snoozed") == "true",
		AssigneeID:     c.Query("assignee_id"),
	}
	for _, status := range c.QueryArray("status") {
		filters.Statuses = append(filters.Statuses, casepkg.Status(status))
	}
	for _, raw := range c.QueryArray("inbox_id") {
		id, err := uuid.Parse(raw)
		if err == nil {
			filters.InboxIDs = append(filters.InboxIDs, id)
		}
	}
	items, err := h.service.ListCases(c.Request.Context(), tid, filters, limitQuery(c, 100))
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"cases": items})
}

func (h CaseHandler) Get(c *gin.Context) {
	tid, caseID, ok := tenantAndCase(c)
	if !ok {
		return
	}
	item, err := h.service.GetCase(c.Request.Context(), tid, caseID)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"case": item})
}

func (h CaseHandler) Create(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	var body struct {
		InboxID     uuid.UUID    `json:"inbox_id"`
		Name        string       `json:"name"`
		AssigneeID  *string      `json:"assignee_id"`
		Type        casepkg.Type `json:"type"`
		DecisionIDs []uuid.UUID  `json:"decision_ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	item, err := h.service.CreateCase(c.Request.Context(), service.CreateCaseInput{
		TenantID: tid, InboxID: body.InboxID, Name: body.Name, AssigneeID: body.AssigneeID, Type: body.Type, DecisionIDs: body.DecisionIDs,
	}, actorID(c))
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"case": item})
}

func (h CaseHandler) Update(c *gin.Context) {
	tid, caseID, ok := tenantAndCase(c)
	if !ok {
		return
	}
	var body struct {
		InboxID     *uuid.UUID       `json:"inbox_id"`
		Name        *string          `json:"name"`
		Status      *casepkg.Status  `json:"status"`
		Outcome     *casepkg.Outcome `json:"outcome"`
		BoostReason *string          `json:"boost_reason"`
		ReviewLevel *string          `json:"review_level"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	item, err := h.service.UpdateCase(c.Request.Context(), service.UpdateCaseInput{
		TenantID: tid, CaseID: caseID, InboxID: body.InboxID, Name: body.Name, Status: body.Status,
		Outcome: body.Outcome, BoostReason: body.BoostReason, ReviewLevel: body.ReviewLevel,
	}, actorID(c))
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"case": item})
}

func (h CaseHandler) AddDecision(c *gin.Context) {
	tid, caseID, ok := tenantAndCase(c)
	if !ok {
		return
	}
	var body struct {
		DecisionID uuid.UUID  `json:"decision_id"`
		ScenarioID *uuid.UUID `json:"scenario_id"`
		ObjectType string     `json:"object_type"`
		ObjectID   string     `json:"object_id"`
		PivotValue *string    `json:"pivot_value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	item, err := h.service.AddDecision(c.Request.Context(), service.AddDecisionInput{
		TenantID: tid, CaseID: caseID, DecisionID: body.DecisionID, ScenarioID: body.ScenarioID,
		ObjectType: body.ObjectType, ObjectID: body.ObjectID, PivotValue: body.PivotValue,
	}, actorID(c))
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"decision": item})
}

func (h CaseHandler) ListEvents(c *gin.Context) {
	tid, caseID, ok := tenantAndCase(c)
	if !ok {
		return
	}
	items, err := h.service.ListEvents(c.Request.Context(), tid, caseID, limitQuery(c, 100))
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": items})
}

func (h CaseHandler) CreateComment(c *gin.Context) {
	tid, caseID, ok := tenantAndCase(c)
	if !ok {
		return
	}
	var body struct {
		Comment string `json:"comment"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	item, err := h.service.CreateComment(c.Request.Context(), tid, caseID, body.Comment, actorID(c))
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"comment": item})
}

func (h CaseHandler) AddTag(c *gin.Context) {
	tid, caseID, ok := tenantAndCase(c)
	if !ok {
		return
	}
	var body struct {
		TagID uuid.UUID `json:"tag_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	if err := h.service.AddTag(c.Request.Context(), tid, caseID, body.TagID, actorID(c)); err != nil {
		presentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h CaseHandler) RemoveTag(c *gin.Context) {
	tid, caseID, ok := tenantAndCase(c)
	if !ok {
		return
	}
	tagID, ok := pathUUID(c, "tagId")
	if !ok {
		return
	}
	if err := h.service.RemoveTag(c.Request.Context(), tid, caseID, tagID, actorID(c)); err != nil {
		presentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h CaseHandler) AddFile(c *gin.Context) {
	tid, caseID, ok := tenantAndCase(c)
	if !ok {
		return
	}
	var body struct {
		FileName    string `json:"file_name"`
		ContentType string `json:"content_type"`
		FileSize    int64  `json:"file_size"`
		StorageKey  string `json:"storage_key"`
		UploadedBy  string `json:"uploaded_by"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	item, err := h.service.AddFile(c.Request.Context(), casepkg.File{
		TenantID: tid, CaseID: caseID, FileName: body.FileName, ContentType: body.ContentType,
		FileSize: body.FileSize, StorageKey: body.StorageKey, UploadedBy: body.UploadedBy,
	}, actorID(c))
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"file": item})
}

func (h CaseHandler) Assign(c *gin.Context) {
	tid, caseID, ok := tenantAndCase(c)
	if !ok {
		return
	}
	var body struct {
		AssigneeID string `json:"assignee_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	if err := h.service.Assign(c.Request.Context(), tid, caseID, &body.AssigneeID, actorID(c)); err != nil {
		presentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h CaseHandler) Unassign(c *gin.Context) {
	tid, caseID, ok := tenantAndCase(c)
	if !ok {
		return
	}
	if err := h.service.Assign(c.Request.Context(), tid, caseID, nil, actorID(c)); err != nil {
		presentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h CaseHandler) Snooze(c *gin.Context) {
	tid, caseID, ok := tenantAndCase(c)
	if !ok {
		return
	}
	var body struct {
		Until time.Time `json:"until"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	if err := h.service.Snooze(c.Request.Context(), tid, caseID, &body.Until, actorID(c)); err != nil {
		presentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h CaseHandler) Unsnooze(c *gin.Context) {
	tid, caseID, ok := tenantAndCase(c)
	if !ok {
		return
	}
	if err := h.service.Snooze(c.Request.Context(), tid, caseID, nil, actorID(c)); err != nil {
		presentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h CaseHandler) Escalate(c *gin.Context) {
	tid, caseID, ok := tenantAndCase(c)
	if !ok {
		return
	}
	var body struct {
		InboxID uuid.UUID `json:"inbox_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	item, err := h.service.UpdateCase(c.Request.Context(), service.UpdateCaseInput{TenantID: tid, CaseID: caseID, InboxID: &body.InboxID}, actorID(c))
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"case": item})
}

func tenantAndCase(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	tid, ok := tenantID(c)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	caseID, ok := pathUUID(c, "caseId")
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	return tid, caseID, true
}
