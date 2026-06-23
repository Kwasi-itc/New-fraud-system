package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
)

type InboxHandler struct {
	service service.CaseService
}

func NewInboxHandler(service service.CaseService) InboxHandler {
	return InboxHandler{service: service}
}

func (h InboxHandler) List(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	items, err := h.service.ListInboxes(c.Request.Context(), tid)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"inboxes": items})
}

func (h InboxHandler) Get(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	inboxID, ok := pathUUID(c, "inboxId")
	if !ok {
		return
	}
	item, err := h.service.GetInbox(c.Request.Context(), tid, inboxID)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"inbox": item})
}

func (h InboxHandler) Create(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	var body struct {
		Name              string     `json:"name"`
		EscalationInboxID *uuid.UUID `json:"escalation_inbox_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	item, err := h.service.CreateInbox(c.Request.Context(), service.CreateInboxInput{
		TenantID: tid, Name: body.Name, EscalationInboxID: body.EscalationInboxID,
	})
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"inbox": item})
}

func (h InboxHandler) Update(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	inboxID, ok := pathUUID(c, "inboxId")
	if !ok {
		return
	}
	var body struct {
		Name                    *string    `json:"name"`
		Status                  *string    `json:"status"`
		EscalationInboxID       *uuid.UUID `json:"escalation_inbox_id"`
		AutoAssignEnabled       *bool      `json:"auto_assign_enabled"`
		CaseReviewManual        *bool      `json:"case_review_manual"`
		CaseReviewOnCaseCreated *bool      `json:"case_review_on_case_created"`
		CaseReviewOnEscalate    *bool      `json:"case_review_on_escalate"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		presentError(c, err)
		return
	}
	item, err := h.service.UpdateInbox(c.Request.Context(), service.UpdateInboxInput{
		TenantID: tid, InboxID: inboxID, Name: body.Name, Status: body.Status, EscalationInboxID: body.EscalationInboxID,
		AutoAssignEnabled: body.AutoAssignEnabled, CaseReviewManual: body.CaseReviewManual,
		CaseReviewOnCaseCreated: body.CaseReviewOnCaseCreated, CaseReviewOnEscalate: body.CaseReviewOnEscalate,
	})
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"inbox": item})
}
