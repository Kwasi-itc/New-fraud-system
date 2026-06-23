package ast_eval

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"golang.org/x/sync/singleflight"
)

type EvaluationCache struct {
	values sync.Map
	group  singleflight.Group
}

func NewEvaluationCache() *EvaluationCache {
	return &EvaluationCache{}
}

func (c *EvaluationCache) evaluate(ctx context.Context, node domainast.Node, runtime Runtime, compute func() (any, error)) (any, error) {
	if c == nil {
		return compute()
	}
	key, ok := evaluationCacheKey(node, runtime)
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

func evaluationCacheKey(node domainast.Node, runtime Runtime) (string, bool) {
	payload := struct {
		TenantID   string         `json:"tenant_id"`
		ObjectID   string         `json:"object_id"`
		ObjectType string         `json:"object_type"`
		Fields     map[string]any `json:"fields"`
		NowUnixNs  int64          `json:"now_unix_ns"`
		Node       domainast.Node `json:"node"`
	}{
		TenantID:   runtime.TenantID,
		ObjectID:   runtime.ObjectID,
		ObjectType: runtime.ObjectType,
		Fields:     runtime.Fields,
		NowUnixNs:  runtime.Now.UnixNano(),
		Node:       node,
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("%x", bytes), true
}
