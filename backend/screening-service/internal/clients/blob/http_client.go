package blob

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

type HTTPClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPClient(baseURL string, timeout time.Duration) HTTPClient {
	return HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

func (c HTTPClient) CreateUploadSession(ctx context.Context, tenantID, screeningID, fileName, contentType string, fileSize int64) (ports.BlobUploadSession, error) {
	if c.baseURL == "" {
		return ports.BlobUploadSession{
			StorageKey: fmt.Sprintf("%s/%s/%s", tenantID, screeningID, fileName),
			UploadURL:  "",
			Method:     http.MethodPut,
		}, nil
	}
	body, err := json.Marshal(map[string]any{
		"tenant_id":    tenantID,
		"screening_id": screeningID,
		"file_name":    fileName,
		"content_type": contentType,
		"file_size":    fileSize,
	})
	if err != nil {
		return ports.BlobUploadSession{}, fmt.Errorf("marshal upload session request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/blob/upload-sessions", bytes.NewReader(body))
	if err != nil {
		return ports.BlobUploadSession{}, fmt.Errorf("create blob request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return ports.BlobUploadSession{}, fmt.Errorf("execute blob request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return ports.BlobUploadSession{}, fmt.Errorf("unexpected status from blob-service: %d", resp.StatusCode)
	}
	var payload struct {
		Session ports.BlobUploadSession `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ports.BlobUploadSession{}, fmt.Errorf("decode blob response: %w", err)
	}
	return payload.Session, nil
}

func (c HTTPClient) GetDownloadURL(ctx context.Context, storageKey string) (ports.BlobDownload, error) {
	if c.baseURL == "" {
		return ports.BlobDownload{DownloadURL: storageKey}, nil
	}
	body, err := json.Marshal(map[string]any{
		"storage_key": storageKey,
	})
	if err != nil {
		return ports.BlobDownload{}, fmt.Errorf("marshal download request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/blob/download-urls", bytes.NewReader(body))
	if err != nil {
		return ports.BlobDownload{}, fmt.Errorf("create blob request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return ports.BlobDownload{}, fmt.Errorf("execute blob request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return ports.BlobDownload{}, fmt.Errorf("unexpected status from blob-service: %d", resp.StatusCode)
	}
	var payload struct {
		Download ports.BlobDownload `json:"download"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ports.BlobDownload{}, fmt.Errorf("decode blob response: %w", err)
	}
	return payload.Download, nil
}
