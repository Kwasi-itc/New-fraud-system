package httpapi

import (
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/access"
	investigator "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

type platformVerifier interface {
	VerifyPlatformSession(string) (investigator.Principal, error)
}
type routePolicy struct {
	param string
	write bool
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

func bindPlatformGrant(db access.Queryer) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := investigator.PrincipalFromContext(c)
		if p == nil {
			platformError(c, 401, "Sign in required.")
			return
		}
		policy, ok := platformRoutePolicies[c.Request.Method+" "+c.FullPath()]
		if !ok {
			c.Next()
			return
		}
		id, err := uuid.Parse(c.Param(policy.param))
		if err != nil || id == uuid.Nil {
			platformError(c, 400, "Invalid resource identifier.")
			return
		}
		grant := access.Grant{Principal: *p, Write: policy.write, Kind: policy.param, ID: id}
		if db == nil {
			platformError(c, 503, "Resource authorization is unavailable.")
			return
		}
		if err = access.Check(c.Request.Context(), db, grant, false); err != nil {
			status := 503
			var denial *access.Error
			if errors.As(err, &denial) {
				status = denial.Status
			}
			platformError(c, status, "Resource access denied.")
			return
		}
		c.Request = c.Request.WithContext(access.WithGrant(c.Request.Context(), grant))
		c.Next()
	}
}
