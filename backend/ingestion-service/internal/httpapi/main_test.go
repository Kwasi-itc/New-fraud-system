package httpapi

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	// Gin's mode is process-global; parallel tests must not mutate it.
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}
