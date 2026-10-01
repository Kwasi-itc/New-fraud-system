package handlers

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"net/http"
	"strconv"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func tenantID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("tenantId"))
	if err != nil || id == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tenantId"})
		return uuid.Nil, false
	}
	return id, true
}

func pathUUID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil || id == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return uuid.Nil, false
	}
	return id, true
}

func limitQuery(c *gin.Context, fallback int) int {
	if c.Query("limit") == "" {
		return fallback
	}
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		return 0 // The service rejects invalid limits instead of silently changing the query.
	}
	return limit
}

func actorID(c *gin.Context) *string {
	return access.Actor(c.Request.Context())
}

func presentError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	status, code, message := http.StatusInternalServerError, "internal_error", "internal server error"
	var dbError *pgconn.PgError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		status, code, message = http.StatusGatewayTimeout, "timeout", "request timed out"
	case errors.Is(err, casepkg.ErrValidation):
		status, code, message = http.StatusBadRequest, "validation_error", err.Error()
	case errors.Is(err, casepkg.ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		status, code, message = http.StatusNotFound, "not_found", "not found"
	case errors.Is(err, casepkg.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "forbidden"
	case errors.Is(err, casepkg.ErrConflict):
		status, code, message = http.StatusConflict, "conflict", "request conflicts with current state"
	case errors.As(err, &dbError):
		switch dbError.Code {
		case "23505", "40001", "40P01", "55P03":
			status, code, message = http.StatusConflict, "conflict", "request conflicts with current state"
		case "23503", "23514", "23502":
			status, code, message = http.StatusBadRequest, "validation_error", "invalid resource reference or value"
			if dbError.ConstraintName == "assignment_capacity" {
				status, code, message = http.StatusConflict, "capacity_reached", "assignee has reached inbox capacity"
			}
		case "57014":
			status, code, message = http.StatusGatewayTimeout, "timeout", "request timed out"
		}
	}
	if status == http.StatusInternalServerError {
		_ = c.Error(err)
	}
	c.JSON(status, gin.H{"error": message, "code": code})
}

func bindJSON(c *gin.Context, body any) error {
	if err := c.ShouldBindJSON(body); err != nil {
		return casepkg.Invalid("invalid JSON request body")
	}
	return nil
}
