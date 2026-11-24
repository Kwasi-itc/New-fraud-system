package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/contract"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ScreeningAuthoringHandler struct {
	Service service.ScreeningAuthoringService
}

func (h ScreeningAuthoringHandler) Handle(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	p := platformauth.PrincipalFromContext(c)
	if p == nil || !p.IsHuman() {
		c.JSON(403, gin.H{"error": "Human operator required."})
		return
	}
	a := workflow.AuthoringActor{UserID: p.Subject, SessionID: p.SessionID.String(), Version: p.AuthzVersion}

	t, sc := c.Param("tenantId"), c.Param("scenarioId")
	for _, raw := range []string{t, sc, a.UserID, a.SessionID} {
		if id, err := uuid.Parse(raw); err != nil || id == uuid.Nil {
			c.JSON(400, gin.H{"error": "Invalid identity or resource identifier."})
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
		if c.ContentType() != "application/json" {
			c.JSON(415, gin.H{"error": "Expected application/json."})
			return
		}
		var in screening.AuthoringRule
		dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, contract.MaxReadRequestBytes))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || dec.Decode(new(any)) != io.EOF || in.ID != c.Param("configId") {
			c.JSON(400, gin.H{"error": "Invalid screening configuration."})
			return
		}
		if err = in.Validate(); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		result, err = h.Service.Save(ctx, t, sc, a, in)
	}
	if err != nil {
		status, message := 500, "Screening configuration is unavailable. Refresh before retrying."
		if errors.Is(err, screening.ErrInlineUnavailable) || errors.Is(err, screening.ErrRecognitionUnavailable) || errors.Is(err, screening.ErrMatchingUnavailable) || errors.Is(err, screening.ErrAddressUnavailable) || errors.Is(err, screening.ErrBirthDateUnavailable) || errors.Is(err, screening.ErrNationalityUnavailable) || errors.Is(err, screening.ErrCountryUnavailable) || errors.Is(err, screening.ErrRegistrationUnavailable) {
			status, message = 422, err.Error()
		}
		var denied *platformauth.Error
		if errors.As(err, &denied) {
			status, message := platformauth.HTTPError(err)
			c.JSON(status, gin.H{"error": message})
			return
		}
		if errors.Is(err, workflow.ErrAuthoringDenied) {
			status, message = 403, "Scenario or inbox access denied."
		}
		if errors.Is(err, screening.ErrAuthoringConflict) {
			status, message = 409, err.Error()
		}
		if errors.Is(err, pgx.ErrNoRows) {
			status, message = 404, "Scenario not found."
		}
		c.JSON(status, gin.H{"error": message})
		return
	}
	c.JSON(200, result)
}
