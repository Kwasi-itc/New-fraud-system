package mysql

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"sync"
)

// Transaction-local bindings for rows inserted by this owner, never authority
// on an existing decision. The pointer is shared by repositories in one unit.
type decisionBindingCache struct {
	mu     sync.Mutex
	values map[string]mysqlstore.UUID
}

func newDecisionBindingCache() *decisionBindingCache {
	return &decisionBindingCache{values: make(map[string]mysqlstore.UUID)}
}
func (c *decisionBindingCache) set(id string, iteration mysqlstore.UUID) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[id] = iteration
}
func (c *decisionBindingCache) get(id string) (mysqlstore.UUID, bool) {
	if c == nil {
		return mysqlstore.UUID{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.values[id]
	return value, ok
}
