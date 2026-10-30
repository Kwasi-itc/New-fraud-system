package httpapi

import (
	"github.com/gin-gonic/gin"
	"os"
	"testing"
)

// Set the process-global Gin mode once, before parallel tests start.
func TestMain(m *testing.M) { gin.SetMode(gin.TestMode); os.Exit(m.Run()) }
