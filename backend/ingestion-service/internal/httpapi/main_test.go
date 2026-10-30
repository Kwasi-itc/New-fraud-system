package httpapi

import (
	"github.com/gin-gonic/gin"
	"os"
	"testing"
)

// Gin mode is global; configure it before any parallel tests run.
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}
