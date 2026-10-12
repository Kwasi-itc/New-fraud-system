package httpapi

import (
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/access"
	"github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"strings"
)

type platformVerifier interface {
	VerifyPlatformSession(string) (investigator.Principal, error)
}
type routePolicy struct {
	param string
	write bool
}

func authorizePlatform(c *gin.Context, verifier platformVerifier, db access.Queryer) {
	c.Header("Cache-Control", "private, no-store")
	token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	if !ok || verifier == nil {
		platformError(c, 401, "Sign in required.")
		return
	}
	principal, err := verifier.VerifyPlatformSession(token)
	if err != nil {
		platformError(c, 401, "Invalid platform credential.")
		return
	}
	policy, ok := platformRoutePolicies[c.Request.Method+" "+c.FullPath()]
	if !ok {
		platformError(c, 403, "This operation is not available to platform users.")
		return
	}
	id, err := uuid.Parse(c.Param(policy.param))
	if err != nil || id == uuid.Nil {
		platformError(c, 400, "Invalid resource identifier.")
		return
	}
	grant := access.Grant{Principal: principal, Write: policy.write, Kind: policy.param, ID: id}
	if db == nil {
		platformError(c, 503, "Identity verification is unavailable.")
		return
	}
	if err := access.Check(c.Request.Context(), db, grant, false); err != nil {
		var denial *access.Error
		if errors.As(err, &denial) {
			platformError(c, denial.Status, denial.Message)
		} else {
			platformError(c, 503, "Authorization is unavailable.")
		}
		return
	}
	c.Request = c.Request.WithContext(access.WithGrant(c.Request.Context(), grant))
	c.Next()
}
func platformError(c *gin.Context, status int, message string) {
	code := "forbidden"
	if status == http.StatusUnauthorized {
		code = "unauthorized"
	}
	if status == http.StatusServiceUnavailable {
		code = "unavailable"
	}
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
