package ast_eval

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"golang.org/x/sync/singleflight"
)

type AggregateResultCache struct {
	values sync.Map
	group  singleflight.Group
}

func NewAggregateResultCache() *AggregateResultCache {
	return &AggregateResultCache{}
}

func (c *AggregateResultCache) evaluate(ctx context.Context, tenantID string, query ports.AggregateQuery, compute func() (any, error)) (any, error) {
	if c == nil {
		return compute()
	}
	key, ok := aggregateResultCacheKey(tenantID, query)
	if !ok {
		return compute()
	}
	if cached, ok := c.values.Load(key); ok {
		return cached, nil
	}
	value, err, _ := c.group.Do(key, func() (any, error) {
		if cached, ok := c.values.Load(key); ok {
			return cached, nil
		}
		value, err := compute()
		if err != nil {
			return nil, err
		}
		c.values.Store(key, value)
		return value, nil
	})
	return value, err
}

func aggregateResultCacheKey(tenantID string, query ports.AggregateQuery) (string, bool) {
	payload := struct {
		TenantID string              `json:"tenant_id"`
		Query    ports.AggregateQuery `json:"query"`
	}{
		TenantID: tenantID,
		Query:    query,
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("%x", bytes), true
}
