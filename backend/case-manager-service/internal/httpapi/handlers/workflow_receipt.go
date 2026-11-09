package handlers

import (
	"context"
	"encoding/json"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
)

type WorkflowReceiptOwner interface {
	DeliverWorkflowReceipt(context.Context, service.WorkflowDispatchRequest) (service.WorkflowDispatchReceipt, error)
	ReconcileWorkflowReceipt(context.Context, service.WorkflowDispatchRequest) (service.WorkflowDispatchReceipt, error)
}

type WorkflowReceiptHandler struct{ Owner WorkflowReceiptOwner }

func (h WorkflowReceiptHandler) Deliver(c *gin.Context)   { serveWorkflowReceipt(c, h.Owner, false) }
func (h WorkflowReceiptHandler) Reconcile(c *gin.Context) { serveWorkflowReceipt(c, h.Owner, true) }

func (h IntegrationHandler) workflowReceipt(c *gin.Context, reconcile bool) {
	serveWorkflowReceipt(c, h.service, reconcile)
}

func serveWorkflowReceipt(c *gin.Context, owner WorkflowReceiptOwner, reconcile bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (1<<20)+4096)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var request service.WorkflowDispatchRequest
	var extra any
	if err := decoder.Decode(&request); err != nil {
		presentError(c, casepkg.Invalid("invalid workflow receipt request"))
		return
	}
	if err := decoder.Decode(&extra); err != io.EOF {
		presentError(c, casepkg.Invalid("invalid workflow receipt request"))
		return
	}
	var receipt service.WorkflowDispatchReceipt
	var err error
	if reconcile {
		receipt, err = owner.ReconcileWorkflowReceipt(c.Request.Context(), request)
	} else {
		receipt, err = owner.DeliverWorkflowReceipt(c.Request.Context(), request)
	}
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, receipt)
}
func (h IntegrationHandler) DeliverWorkflowReceipt(c *gin.Context)   { h.workflowReceipt(c, false) }
func (h IntegrationHandler) ReconcileWorkflowReceipt(c *gin.Context) { h.workflowReceipt(c, true) }
