package datamodel

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

func (c *HTTPClient) ListIndexJobsForTables(ctx context.Context, tenant string, tables []string, limit int) ([]ports.ManagedIndexJob, error) {
	id, err := uuid.Parse(tenant)
	if err != nil || id == uuid.Nil || len(tables) == 0 || len(tables) > 100 || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid bounded index-job scope")
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	allowed := map[string]bool{}
	for _, table := range tables {
		id, err := uuid.Parse(table)
		if err != nil || id == uuid.Nil || allowed[id.String()] {
			return nil, fmt.Errorf("invalid table scope")
		}
		allowed[id.String()] = true
		query.Add("table_id", id.String())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/tenants/"+id.String()+"/index-jobs/scoped?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	attachRequestID(req)
	response, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("index preparation owner returned status %d", response.StatusCode)
	}
	const maxBytes = 8 << 20
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxBytes {
		return nil, fmt.Errorf("index preparation response exceeds byte limit")
	}
	var payload listIndexJobsResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if payload.IndexJobs == nil || len(payload.IndexJobs) > limit {
		return nil, fmt.Errorf("incomplete or oversized index preparation response")
	}
	out := make([]ports.ManagedIndexJob, 0, len(payload.IndexJobs))
	seen := map[string]bool{}
	for _, job := range payload.IndexJobs {
		jobID, parseErr := uuid.Parse(job.ID)
		if parseErr != nil || jobID == uuid.Nil || job.TenantID != id.String() || !allowed[job.TableID] || seen[job.ID] {
			return nil, fmt.Errorf("index preparation owner returned an invalid scope")
		}
		seen[job.ID] = true
		out = append(out, ports.ManagedIndexJob{ID: job.ID, TenantID: job.TenantID, TableID: job.TableID, TableName: job.TableName, IndexType: job.IndexType, Status: job.Status, Columns: job.Columns})
	}
	return out, nil
}
