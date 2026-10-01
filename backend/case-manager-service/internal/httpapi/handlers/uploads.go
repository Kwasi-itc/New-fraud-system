package handlers

import (
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/gin-gonic/gin"
	"io"
	"mime"
	"net/http"
)

func (h CaseHandler) StartUpload(c *gin.Context) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	var file casepkg.File
	if err := bindJSON(c, &file); err != nil {
		presentError(c, err)
		return
	}
	file.TenantID = tenant
	file.CaseID = id
	upload, err := h.service.StartUpload(c.Request.Context(), file)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"upload": upload})
}
func (h CaseHandler) UploadContent(c *gin.Context) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	upload, ok := pathUUID(c, "uploadId")
	if !ok {
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, casepkg.MaxEvidenceBytes))
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "upload exceeds 10 MiB or is incomplete"})
		return
	}
	if err := h.service.UploadContent(c.Request.Context(), tenant, id, upload, data); err != nil {
		presentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (h CaseHandler) FinalizeUpload(c *gin.Context) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	upload, ok := pathUUID(c, "uploadId")
	if !ok {
		return
	}
	file, err := h.service.FinalizeUpload(c.Request.Context(), tenant, id, upload)
	if err != nil {
		presentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"file": file})
}
func (h CaseHandler) DownloadEvidence(c *gin.Context) {
	tenant, id, ok := tenantAndCase(c)
	if !ok {
		return
	}
	file, ok := pathUUID(c, "fileId")
	if !ok {
		return
	}
	upload, err := h.service.DownloadEvidence(c.Request.Context(), tenant, id, file)
	if err != nil {
		presentError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": upload.FileName}))
	c.Header("Content-Security-Policy", "sandbox")
	c.Data(http.StatusOK, upload.ContentType, upload.Data)
}
