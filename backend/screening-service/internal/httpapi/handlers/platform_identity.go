package handlers

import (
	"errors"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/gin-gonic/gin"
)

func humanActor(c *gin.Context) (string, bool) {
	p := auth.PrincipalFromContext(c)
	if p == nil || !p.IsHuman() {
		c.AbortWithStatusJSON(403, gin.H{"error": "Human identity required."})
		return "", false
	}
	return p.Subject, true
}
func screeningError(c *gin.Context, err error) {
	var denied *auth.Error
	if errors.As(err, &denied) {
		code, message := auth.HTTPError(err)
		c.JSON(code, gin.H{"error": message})
		return
	}
	c.JSON(400, gin.H{"error": err.Error()})
}
