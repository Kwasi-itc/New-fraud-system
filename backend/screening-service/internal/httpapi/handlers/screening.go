package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
)

type ScreeningHandler struct {
	service service.ScreeningService
}

func NewScreeningHandler(service service.ScreeningService) ScreeningHandler {
	return ScreeningHandler{service: service}
}

type createScreeningRequest struct {
	Provider                     string                  `json:"provider"`
	DecisionID                   string                  `json:"decision_id"`
	ScenarioID                   string                  `json:"scenario_id"`
	ScreeningConfigID            string                  `json:"screening_config_id"`
	IdempotencyKey               string                  `json:"idempotency_key"`
	ObjectType                   string                  `json:"object_type"`
	ObjectID                     string                  `json:"object_id"`
	Queries                      []screening.SearchQuery `json:"queries"`
	ProviderConfig               map[string]any          `json:"provider_config"`
	LimitOverride                *int                    `json:"limit_override"`
	UniqueCounterpartyIdentifier *string                 `json:"unique_counterparty_identifier"`
}

type reviewMatchRequest struct {
	Status     string `json:"status"`
	Comment    string `json:"comment"`
	ReviewerID string `json:"reviewer_id"`
	Whitelist  bool   `json:"whitelist"`
}

type addCommentRequest struct {
	Comment  string `json:"comment"`
	AuthorID string `json:"author_id"`
}

type createFileRequest struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	FileSize    int64  `json:"file_size"`
	StorageKey  string `json:"storage_key"`
	UploadedBy  string `json:"uploaded_by"`
}

type createFileUploadRequest struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	FileSize    int64  `json:"file_size"`
	UploadedBy  string `json:"uploaded_by"`
}

func (h ScreeningHandler) Create(c *gin.Context) {
	var body createScreeningRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	request := screening.SearchRequest{
		Provider:                     body.Provider,
		DecisionID:                   body.DecisionID,
		ScenarioID:                   body.ScenarioID,
		ScreeningConfigID:            body.ScreeningConfigID,
		IdempotencyKey:               body.IdempotencyKey,
		ObjectType:                   body.ObjectType,
		ObjectID:                     body.ObjectID,
		Queries:                      body.Queries,
		LimitOverride:                body.LimitOverride,
		UniqueCounterpartyIdentifier: body.UniqueCounterpartyIdentifier,
	}
	if body.ProviderConfig != nil {
		raw, err := jsonMarshal(body.ProviderConfig)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		request.ProviderConfig = raw
	}

	item, err := h.service.CreateScreening(c.Request.Context(), c.Param("tenantId"), request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h ScreeningHandler) CreateFreeform(c *gin.Context) {
	var body createScreeningRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	request := screening.SearchRequest{
		Provider:                     body.Provider,
		IdempotencyKey:               body.IdempotencyKey,
		ObjectType:                   body.ObjectType,
		ObjectID:                     body.ObjectID,
		Queries:                      body.Queries,
		LimitOverride:                body.LimitOverride,
		UniqueCounterpartyIdentifier: body.UniqueCounterpartyIdentifier,
		IsManual:                     true,
	}
	if body.ProviderConfig != nil {
		raw, err := jsonMarshal(body.ProviderConfig)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		request.ProviderConfig = raw
	}

	item, err := h.service.CreateFreeformScreening(c.Request.Context(), c.Param("tenantId"), request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h ScreeningHandler) ListByDecision(c *gin.Context) {
	items, err := h.service.ListByDecision(c.Request.Context(), c.Param("tenantId"), c.Param("decisionId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h ScreeningHandler) Get(c *gin.Context) {
	item, err := h.service.GetDetails(c.Request.Context(), c.Param("tenantId"), c.Param("screeningId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h ScreeningHandler) Retry(c *gin.Context) {
	item, err := h.service.Retry(c.Request.Context(), c.Param("tenantId"), c.Param("screeningId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h ScreeningHandler) ReviewMatch(c *gin.Context) {
	var body reviewMatchRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.service.ReviewMatch(c.Request.Context(), c.Param("tenantId"), c.Param("matchId"), body.Status, body.Comment, body.ReviewerID, body.Whitelist)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h ScreeningHandler) AddComment(c *gin.Context) {
	var body addCommentRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.service.AddComment(c.Request.Context(), c.Param("tenantId"), c.Param("matchId"), body.Comment, body.AuthorID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h ScreeningHandler) EnrichMatch(c *gin.Context) {
	item, err := h.service.EnrichMatch(c.Request.Context(), c.Param("tenantId"), c.Param("matchId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h ScreeningHandler) CreateFile(c *gin.Context) {
	var body createFileRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.service.CreateFile(c.Request.Context(), c.Param("tenantId"), c.Param("screeningId"), body.FileName, body.ContentType, body.StorageKey, body.UploadedBy, body.FileSize)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h ScreeningHandler) ListFiles(c *gin.Context) {
	items, err := h.service.ListFiles(c.Request.Context(), c.Param("tenantId"), c.Param("screeningId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h ScreeningHandler) GetFile(c *gin.Context) {
	item, err := h.service.GetFile(c.Request.Context(), c.Param("tenantId"), c.Param("screeningId"), c.Param("fileId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h ScreeningHandler) CreateFileUpload(c *gin.Context) {
	var body createFileUploadRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	file, session, err := h.service.CreateFileUpload(c.Request.Context(), c.Param("tenantId"), c.Param("screeningId"), body.FileName, body.ContentType, body.UploadedBy, body.FileSize)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"file":    file,
		"session": session,
	})
}

func (h ScreeningHandler) GetFileDownload(c *gin.Context) {
	item, err := h.service.GetFileDownload(c.Request.Context(), c.Param("tenantId"), c.Param("screeningId"), c.Param("fileId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func jsonMarshal(input any) ([]byte, error) {
	return json.Marshal(input)
}
