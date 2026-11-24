package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// CaseEvidence checks the case owner's tenant, link, and inbox read contract.
// Signed capabilities remain stateless; ordinary reads do not requery identity.
func CaseEvidence(db Queryer, owner, resourceParam string, next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := PrincipalFromContext(c)
		if p == nil || !p.IsHuman() || !p.Can("cases:read") {
			c.AbortWithStatus(403)
			return
		}
		id, err := uuid.Parse(c.Param("caseId"))
		if err != nil || id == uuid.Nil {
			c.AbortWithStatus(400)
			return
		}
		if db == nil {
			c.AbortWithStatus(503)
			return
		}
		var allowed bool
		var caseStatus, inboxStatus string
		err = db.QueryRow(c.Request.Context(), `SELECT allowed,case_status,inbox_status FROM case_manager.authorize_case_access_v1($1,$2,$3,$4,$5,$6,$7,false,$8,'cases:read')`, p.TenantID, id, p.Subject, owner, c.Param(resourceParam), p.SessionID, p.AuthzVersion, p.Can("cases:access-all")).Scan(&allowed, &caseStatus, &inboxStatus)
		if err != nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "case_authority_unavailable"})
			return
		}
		if !allowed {
			c.AbortWithStatusJSON(403, gin.H{"error": "case_access_denied"})
			return
		}
		next(c)
	}
}
