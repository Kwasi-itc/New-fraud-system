package mysqlruntime

import (
	"database/sql"

	"github.com/gin-gonic/gin"
)

func registerLocalHealth(router *gin.Engine, db *sql.DB) {
	router.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	router.GET("/readyz", localReadyHandler(db))
}
func localReadyHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var count int
		err := db.QueryRowContext(c.Request.Context(), "SELECT COUNT(*) FROM fraud.data_model_tenants WHERE id=UNHEX('00000000000000000000000000000000')").Scan(&count)
		if err != nil {
			c.JSON(503, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(200, gin.H{"status": "ready", "database": "mysql", "mode": "local-development"})
	}
}
