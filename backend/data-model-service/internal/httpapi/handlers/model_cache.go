package handlers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Hash the complete representation, including tenant writability and enum values.
// The migration-derived revision alone is not a complete cache validator.
// Always assemble the current model before evaluating the precondition.
func writeConditionalModel(c *gin.Context, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "internal_error", "message": "encode data model"}})
		return
	}
	etag := fmt.Sprintf(`"%x"`, sha256.Sum256(body))
	c.Header("ETag", etag)
	c.Header("Cache-Control", "private, no-cache")
	for _, header := range c.Request.Header.Values("If-None-Match") {
		for _, candidate := range strings.Split(header, ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate == "*" || strings.TrimPrefix(candidate, "W/") == etag {
				c.Status(http.StatusNotModified)
				return
			}
		}
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}
