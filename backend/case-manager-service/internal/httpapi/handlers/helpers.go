package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func tenantID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tenantId"})
		return uuid.Nil, false
	}
	return id, true
}

func pathUUID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
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
		return fallback
	}
	return limit
}

func actorID(c *gin.Context) *string {
	value := c.GetHeader("X-Actor-ID")
	if value == "" {
		return nil
	}
	return &value
}

func presentError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}
