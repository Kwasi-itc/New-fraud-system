package datamodel

import (
	"container/list"
	"sync"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/google/uuid"
)

const defaultCacheEntries = 128
const defaultCacheBytes = 16 << 20

type cacheEntry struct {
	tenantID     uuid.UUID
	objectType   string
	etag         string
	model        ingestion.PublishedDataModel
	encodedBytes int
}

type modelCacheKey struct {
	tenantID   uuid.UUID
	objectType string
}

// modelCache owns immutable models. Callers receive deep copies. The byte budget
// bounds retained encoded-model size; decoded Go objects have additional overhead.
// No entry may be used without successful origin revalidation on that request.
type modelCache struct {
	mu                          sync.Mutex
	entries                     map[modelCacheKey]*list.Element
	lru                         *list.List
	maxEntries, maxBytes, bytes int
}

func newModelCache(maxEntries, maxBytes int) *modelCache {
	return &modelCache{entries: make(map[modelCacheKey]*list.Element), lru: list.New(), maxEntries: maxEntries, maxBytes: maxBytes}
}

func (c *modelCache) get(id uuid.UUID) (cacheEntry, bool) {
	return c.getTable(id, "")
}

func (c *modelCache) getTable(id uuid.UUID, objectType string) (cacheEntry, bool) {
	if c == nil {
		return cacheEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[modelCacheKey{id, objectType}]
	if !ok {
		return cacheEntry{}, false
	}
	c.lru.MoveToFront(e)
	return e.Value.(cacheEntry), true
}

func (c *modelCache) put(entry cacheEntry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := modelCacheKey{entry.tenantID, entry.objectType}
	if old, ok := c.entries[key]; ok {
		c.remove(old)
	}
	if c.maxEntries <= 0 || c.maxBytes <= 0 || entry.encodedBytes > c.maxBytes {
		return
	}
	for c.lru.Len() >= c.maxEntries || c.bytes+entry.encodedBytes > c.maxBytes {
		c.remove(c.lru.Back())
	}
	c.entries[key] = c.lru.PushFront(entry)
	c.bytes += entry.encodedBytes
}

func (c *modelCache) remove(e *list.Element) {
	entry := e.Value.(cacheEntry)
	delete(c.entries, modelCacheKey{entry.tenantID, entry.objectType})
	c.bytes -= entry.encodedBytes
	c.lru.Remove(e)
}

func (c *modelCache) forget(id uuid.UUID) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[modelCacheKey{id, ""}]; ok {
		c.remove(e)
	}
}

func cloneModel(model ingestion.PublishedDataModel) ingestion.PublishedDataModel {
	model.ManagedSystemFields = append([]string(nil), model.ManagedSystemFields...)
	tables := make(map[string]ingestion.ObjectSchema, len(model.Tables))
	for name, table := range model.Tables {
		fields := make(map[string]ingestion.FieldSchema, len(table.Fields))
		for name, field := range table.Fields {
			field.EnumValues = append([]ingestion.EnumValue(nil), field.EnumValues...)
			fields[name] = field
		}
		table.Fields = fields
		tables[name] = table
	}
	model.Tables = tables
	return model
}
