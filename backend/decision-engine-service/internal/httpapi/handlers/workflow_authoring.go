package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"strconv"
	"time"
)

type WorkflowAuthoringHandler struct {
	Service service.WorkflowAuthoringService
}

func (h WorkflowAuthoringHandler) Handle(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	a := workflow.AuthoringActor{UserID: c.GetHeader("X-Workflow-User"), SessionID: c.GetHeader("X-Workflow-Session")}
	a.Version, _ = strconv.ParseInt(c.GetHeader("X-Workflow-Version"), 10, 64)
	t, sc := c.Param("tenantId"), c.Param("scenarioId")
	for _, id := range []string{t, sc, a.UserID, a.SessionID} {
		if v, e := uuid.Parse(id); e != nil || v == uuid.Nil {
			c.JSON(400, gin.H{"error": "Invalid identity or resource ID."})
			return
		}
	}
	if a.Version < 1 {
		c.JSON(403, gin.H{"error": "Current membership required."})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	var result any
	var err error
	if c.Request.Method == "GET" {
		result, err = h.Service.List(ctx, t, sc, a)
	} else {
		var in workflow.CaseRule
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
		dec := json.NewDecoder(c.Request.Body)
		dec.DisallowUnknownFields()
		if err = dec.Decode(&in); err != nil {
			c.JSON(400, gin.H{"error": "Invalid workflow configuration."})
			return
		}
		if err = dec.Decode(new(any)); err != io.EOF {
			c.JSON(400, gin.H{"error": "Invalid workflow configuration."})
			return
		}
		if in.ID != c.Param("ruleId") {
			c.JSON(400, gin.H{"error": "Rule ID mismatch."})
			return
		}
		if err = in.Validate(); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		result, err = h.Service.Save(ctx, t, sc, a, in)
	}
	if err != nil {
		status := 500
		message := "Workflow service is unavailable. Refresh before retrying."
		if errors.Is(err, workflow.ErrAuthoringDenied) {
			status = 403
			message = "Workflow or destination inbox access denied."
		}
		if errors.Is(err, workflow.ErrAuthoringConflict) {
			status = 409
			message = err.Error()
		}
		if errors.Is(err, pgx.ErrNoRows) {
			status = 404
			message = "Scenario not found."
		}
		c.JSON(status, gin.H{"error": message})
		return
	}
	c.JSON(200, result)
}
