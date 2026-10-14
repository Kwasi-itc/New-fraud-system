package handlers

import (
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/access"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"log/slog"
	"net/http"
)

type DisplayHandler struct{ Store display.Store }

func displayError(c *gin.Context, err error) {
	var bad *display.Invalid
	var pg *pgconn.PgError
	var denied *access.Error
	switch {
	case errors.As(err, &denied):
		writeError(c, err)
	case errors.As(err, &bad):
		writeBadRequest(c, bad.Error())
	case errors.Is(err, display.ErrConflict):
		c.JSON(409, gin.H{"error": gin.H{"message": err.Error()}})
	case errors.Is(err, pgx.ErrNoRows):
		c.JSON(404, gin.H{"error": gin.H{"message": "Table is unavailable."}})
	case errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01" || pg.Code == "42P01" || pg.Code == "42703"):
		c.JSON(409, gin.H{"error": gin.H{"message": "The data model changed. Reload and retry."}})
	default:
		slog.Error("display operation failed", "error", err)
		c.JSON(503, gin.H{"error": gin.H{"message": "Display data is temporarily unavailable."}})
	}
}
func (h DisplayHandler) Get(c *gin.Context) {
	id, ok := parseUUIDParam(c, "tableId")
	if !ok {
		return
	}
	result, err := h.Store.Get(c.Request.Context(), id)
	if err != nil {
		displayError(c, err)
		return
	}
	c.JSON(200, result)
}
func (h DisplayHandler) Save(c *gin.Context) {
	id, ok := parseUUIDParam(c, "tableId")
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128*1024)
	var in display.SaveRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		writeBadRequest(c, "Invalid display settings.")
		return
	}
	result, err := h.Store.Save(c.Request.Context(), id, in)
	if err != nil {
		displayError(c, err)
		return
	}
	c.JSON(200, result)
}
func (h DisplayHandler) Read(c *gin.Context) {
	id, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128*1024)
	var in struct {
		Objects []display.Object `json:"objects"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		writeBadRequest(c, "Invalid records.")
		return
	}
	result, err := h.Store.Read(c.Request.Context(), id, in.Objects)
	if err != nil {
		displayError(c, err)
		return
	}
	c.JSON(200, result)
}
