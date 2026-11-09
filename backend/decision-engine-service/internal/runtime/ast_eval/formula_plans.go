package ast_eval

import (
	"container/list"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
)

// FormulaPlans caches parsed immutable programs, never evaluated values or
// permissions. Fresh model validation and tenant authorization remain required.
// Scope includes tenant, immutable iteration and model fingerprint; formula
// bytes are hashed too, so mutable drafts cannot reuse another expression.
type FormulaPlans struct {
	mu                          sync.Mutex
	lru                         *list.List
	entries                     map[[32]byte]*list.Element
	bytes, maxBytes, maxEntries int
}
type formulaPlan struct {
	key   [32]byte
	node  domainast.Node
	bytes int
}

// ValidateScope caches only successful pure static validation. The caller must
// fingerprint the complete scenario program AND current model, and must perform
// live authorization/publication checks independently on every execution.
func (c *FormulaPlans) ValidateScope(tenant, scope string, validate func() error) error {
	if c == nil || scope == "" {
		return validate()
	}
	keyBytes, _ := json.Marshal([]string{"validated-program", tenant, scope})
	key := sha256.Sum256(keyBytes)
	c.mu.Lock()
	if entry := c.entries[key]; entry != nil {
		c.lru.MoveToFront(entry)
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	if err := validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries[key] != nil {
		return nil
	}
	const size = 256
	for len(c.entries) >= c.maxEntries || c.bytes+size > c.maxBytes {
		old := c.lru.Back()
		plan := old.Value.(formulaPlan)
		delete(c.entries, plan.key)
		c.bytes -= plan.bytes
		c.lru.Remove(old)
	}
	c.entries[key] = c.lru.PushFront(formulaPlan{key: key, bytes: size})
	c.bytes += size
	return nil
}

func NewFormulaPlans(entries, bytes int) (*FormulaPlans, error) {
	if entries < 1 || entries > 4096 || bytes < 1024 || bytes > 64<<20 {
		return nil, errors.New("bounded formula plan cache required")
	}
	return &FormulaPlans{lru: list.New(), entries: make(map[[32]byte]*list.Element), maxBytes: bytes, maxEntries: entries}, nil
}

func (c *FormulaPlans) parse(formula json.RawMessage, runtime Runtime) (domainast.Node, error) {
	if c == nil || runtime.PlanScope == "" || len(formula) > 1<<20 {
		var n domainast.Node
		err := json.Unmarshal(formula, &n)
		return n, err
	}
	// Length-delimited JSON prevents concatenation ambiguities in scope keys.
	keyBytes, err := json.Marshal([]any{"parsed-expression", runtime.TenantID, runtime.PlanScope, json.RawMessage(formula)})
	if err != nil {
		return domainast.Node{}, err
	}
	key := sha256.Sum256(keyBytes)
	c.mu.Lock()
	if entry := c.entries[key]; entry != nil {
		c.lru.MoveToFront(entry)
		node := entry.Value.(formulaPlan).node
		c.mu.Unlock()
		return node, nil
	}
	c.mu.Unlock()
	var node domainast.Node
	if err := json.Unmarshal(formula, &node); err != nil {
		return node, err
	}
	// Account for AST maps/slices as well as retained source; this deliberately
	// conservative estimate keeps tiny-node JSON from evading the byte bound.
	size := len(formula)*8 + 256
	if size > c.maxBytes {
		return node, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.entries[key]; entry != nil {
		c.lru.MoveToFront(entry)
		return entry.Value.(formulaPlan).node, nil
	}
	for len(c.entries) >= c.maxEntries || c.bytes+size > c.maxBytes {
		old := c.lru.Back()
		plan := old.Value.(formulaPlan)
		delete(c.entries, plan.key)
		c.bytes -= plan.bytes
		c.lru.Remove(old)
	}
	c.entries[key] = c.lru.PushFront(formulaPlan{key, node, size})
	c.bytes += size
	return node, nil
}

// Constants may escape through returned evidence. Detach containers so callers
// cannot mutate a cached program; scalar constants retain their exact types.
func detachedConstant(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = detachedConstant(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = detachedConstant(x)
		}
		return out
	default:
		return value
	}
}
