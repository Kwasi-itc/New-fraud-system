package postgres

import (
	"context"
	"sync"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultRollupDefinitionCacheTTL = 5 * time.Second

type rollupDefCacheKey struct {
	tenantID  string
	tableName string
}

type rollupDefCacheEntry struct {
	defs      []ingestion.RollupDefinition
	expiresAt time.Time
}

// RollupDefinitionCache caches active rollup definitions (capturing, backfilling, ready)
// per (tenantID, tableName) with a short TTL so writes avoid per-row metadata queries.
type RollupDefinitionCache struct {
	pool    *pgxpool.Pool
	ttl     time.Duration
	mu      sync.RWMutex
	entries map[rollupDefCacheKey]rollupDefCacheEntry
}

func NewRollupDefinitionCache(pool *pgxpool.Pool, ttl time.Duration) *RollupDefinitionCache {
	if ttl <= 0 {
		ttl = DefaultRollupDefinitionCacheTTL
	}
	return &RollupDefinitionCache{
		pool:    pool,
		ttl:     ttl,
		entries: make(map[rollupDefCacheKey]rollupDefCacheEntry),
	}
}

func (c *RollupDefinitionCache) Invalidate(tenantID, tableName string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if tableName == "" {
		for k := range c.entries {
			if k.tenantID == tenantID {
				delete(c.entries, k)
			}
		}
		return
	}
	delete(c.entries, rollupDefCacheKey{tenantID: tenantID, tableName: tableName})
}

func (c *RollupDefinitionCache) InvalidateAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[rollupDefCacheKey]rollupDefCacheEntry)
}

func (c *RollupDefinitionCache) GetActiveForTable(ctx context.Context, q txExecutor, tenantID, tableName string) ([]ingestion.RollupDefinition, error) {
	if c == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	key := rollupDefCacheKey{tenantID: tenantID, tableName: tableName}
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		return entry.defs, nil
	}

	const stmt = `
		SELECT tenant_id, id, table_name, group_field, time_field, value_field,
			value_type, model_version, status, backfill_snapshot::text, created_at, updated_at
		FROM core_ingestion.rollup_definitions
		WHERE tenant_id = $1 AND table_name = $2 AND status IN ('capturing', 'backfilling', 'ready')
		ORDER BY created_at
	`
	var rows pgx.Rows
	var err error
	if c.pool != nil {
		rows, err = c.pool.Query(ctx, stmt, tenantID, tableName)
	} else if q != nil {
		rows, err = q.Query(ctx, stmt, tenantID, tableName)
	} else {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var defs []ingestion.RollupDefinition
	for rows.Next() {
		def, err := scanRollupDefinition(rows)
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.entries[key] = rollupDefCacheEntry{
		defs:      defs,
		expiresAt: now.Add(c.ttl),
	}
	c.mu.Unlock()
	return defs, nil
}
