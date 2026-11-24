package httpapi

import (
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"github.com/gin-gonic/gin"
	"io"
	"log/slog"
	"testing"
)

func TestEveryRegisteredAPIRouteRequiresSignedIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SERVICE_AUTH_MODE", "disabled")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(logger, nil, RouterConfig{PolicyObserver:authtest.ObserveRoutePolicies(t,"case-manager-service")})
	authtest.AssertSignedRoutes(t, router)
}
