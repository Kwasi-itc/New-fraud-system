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

type AggregateResultCacheOutcome struct {
	Value          any
	CacheHit       bool
	SharedInflight bool
	RemoteExecuted bool
}

type aggregateResultCacheComputeResult struct {
	Value          any
	CacheHit       bool
	RemoteExecuted bool
}

func NewAggregateResultCache() *AggregateResultCache {
	return &AggregateResultCache{}
}

func (c *AggregateResultCache) evaluate(ctx context.Context, tenantID string, query ports.AggregateQuery, compute func() (any, error)) (AggregateResultCacheOutcome, error) {
	if c == nil {
		value, err := compute()
		return AggregateResultCacheOutcome{Value: value, RemoteExecuted: true}, err
	}
	key, ok := aggregateResultCacheKey(tenantID, query)
	if !ok {
		value, err := compute()
		return AggregateResultCacheOutcome{Value: value, RemoteExecuted: true}, err
	}
	if cached, ok := c.values.Load(key); ok {
		return AggregateResultCacheOutcome{Value: cached, CacheHit: true}, nil
	}
	computeExecuted := false
	value, err, shared := c.group.Do(key, func() (any, error) {
		if cached, ok := c.values.Load(key); ok {
			return aggregateResultCacheComputeResult{Value: cached, CacheHit: true}, nil
		}
		computeExecuted = true
		value, err := compute()
		if err != nil {
			return nil, err
		}
		c.values.Store(key, value)
		return aggregateResultCacheComputeResult{Value: value, RemoteExecuted: true}, nil
	})
	if err != nil {
		return AggregateResultCacheOutcome{}, err
	}
	computeResult, ok := value.(aggregateResultCacheComputeResult)
	if !ok {
		return AggregateResultCacheOutcome{}, fmt.Errorf("aggregate result cache returned unexpected type %T", value)
	}
	return AggregateResultCacheOutcome{
		Value:          computeResult.Value,
		CacheHit:       computeResult.CacheHit,
		SharedInflight: shared && !computeExecuted,
		RemoteExecuted: computeExecuted && computeResult.RemoteExecuted,
	}, nil
}

func aggregateResultCacheKey(tenantID string, query ports.AggregateQuery) (string, bool) {
	payload := struct {
		TenantID string               `json:"tenant_id"`
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
