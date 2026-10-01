package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h InboxHandler) ListUsers(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "inboxId")
	if !ok {
		return
	}
	users, err := h.service.ListInboxUsers(c.Request.Context(), tid, id)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}
func (h InboxHandler) PutUser(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "inboxId")
	if !ok {
		return
	}
	var body struct {
		AutoAssignEnabled bool `json:"auto_assign_enabled"`
		Capacity          *int `json:"capacity"`
	}
	if err := bindJSON(c, &body); err != nil {
		presentError(c, err)
		return
	}
	capacity := 20
	if body.Capacity != nil {
		capacity = *body.Capacity
	}
	user, err := h.service.PutInboxUser(c.Request.Context(), tid, id, c.Param("userId"), body.AutoAssignEnabled, capacity)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}
func (h InboxHandler) RemoveUser(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "inboxId")
	if !ok {
		return
	}
	if err := h.service.RemoveInboxUser(c.Request.Context(), tid, id, c.Param("userId")); err != nil {
		presentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
