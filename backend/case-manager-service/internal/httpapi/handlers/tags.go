package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
)

type TagHandler struct {
	service service.CaseService
}

func NewTagHandler(service service.CaseService) TagHandler {
	return TagHandler{service: service}
}

func (h TagHandler) Update(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "tagId")
	if !ok {
		return
	}
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := bindJSON(c, &body); err != nil {
		presentError(c, err)
		return
	}
	tag, err := h.service.UpdateTag(c.Request.Context(), tid, id, body.Name, body.Color)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tag": tag})
}

func (h TagHandler) Archive(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "tagId")
	if !ok {
		return
	}
	if err := h.service.ArchiveTag(c.Request.Context(), tid, id); err != nil {
		presentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h TagHandler) List(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	items, err := h.service.ListTags(c.Request.Context(), tid, c.Query("target"))
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": items})
}

func (h TagHandler) Create(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	var body struct {
		Name   string `json:"name"`
		Color  string `json:"color"`
		Target string `json:"target"`
	}
	if err := bindJSON(c, &body); err != nil {
		presentError(c, err)
		return
	}
	item, err := h.service.CreateTag(c.Request.Context(), service.CreateTagInput{TenantID: tid, Name: body.Name, Color: body.Color, Target: body.Target})
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"tag": item})
}
