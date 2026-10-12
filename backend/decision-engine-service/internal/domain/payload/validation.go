// Package payload validates full evaluation objects without performing I/O.
package payload

import (
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

const MaxIssues = 100

type Issue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Error struct {
	Category      string  `json:"category"`
	ModelRevision string  `json:"model_revision"`
	Issues        []Issue `json:"validation_errors"`
	Truncated     bool    `json:"truncated"`
}

func (e *Error) Error() string {
	return e.Category + ": evaluation object does not satisfy the data model"
}

// ModelContractError identifies invalid dependency metadata, not caller input.
type ModelContractError struct{}

func (*ModelContractError) Error() string { return "tenant model field contract is invalid" }

type field struct {
	definition ports.TenantModelField
	enum       map[string]struct{}
}
type schema struct {
	fields  map[string]field
	ordered []string
	managed map[string]struct{}
	lookup  string
}
type entry struct {
	schema  *schema
	expires time.Time
	used    uint64
}
type cacheKey struct{ tenant, objectType, revision string }

// Validator keeps compiled definitions bounded. Schemas are immutable after compilation.
type Validator struct {
	mu       sync.Mutex
	entries  map[cacheKey]entry
	sequence uint64
	capacity int
	ttl      time.Duration
	hits     atomic.Uint64
	builds   atomic.Uint64
}

type Metrics struct {
	CacheHits    uint64 `json:"cache_hits"`
	SchemaBuilds uint64 `json:"schema_builds"`
	CacheEntries int    `json:"cache_entries"`
}

func (v *Validator) Metrics() Metrics {
	if v == nil {
		return Metrics{}
	}
	v.mu.Lock()
	size := len(v.entries)
	v.mu.Unlock()
	return Metrics{v.hits.Load(), v.builds.Load(), size}
}

func NewValidator() *Validator {
	return &Validator{entries: make(map[cacheKey]entry), capacity: 128, ttl: 30 * time.Second}
}

func (v *Validator) compiled(tenant, objectType string, model ports.TenantModel) (*schema, error) {
	key := cacheKey{tenant, objectType, model.RevisionID}
	v.mu.Lock()
	defer v.mu.Unlock()
	now := time.Now()
	v.sequence++
	if item, ok := v.entries[key]; ok && now.Before(item.expires) {
		v.hits.Add(1)
		item.used = v.sequence
		v.entries[key] = item
		return item.schema, nil
	}
	table, ok := model.Tables[objectType]
	if !ok || table.Archived {
		return nil, &Error{Category: "payload_validation_failed", ModelRevision: model.RevisionID, Issues: []Issue{{"object_type", "unknown_object_type", "object type is not active in the tenant model"}}}
	}
	s := &schema{fields: make(map[string]field, len(table.Fields)), managed: make(map[string]struct{}), lookup: model.RecordLookupField}
	if s.lookup == "" {
		s.lookup = "object_id"
	}
	for _, name := range model.ManagedSystemFields {
		s.managed[name] = struct{}{}
	}
	// These physical system columns are platform-owned even in legacy contracts.
	for _, name := range []string{"id", "updated_at", "valid_from", "valid_until"} {
		s.managed[name] = struct{}{}
	}
	for name, def := range table.Fields {
		if def.Archived {
			continue
		}
		switch def.Type {
		case "bool", "int", "float", "timestamp", "ip_address", "string":
		default:
			return nil, &ModelContractError{}
		}
		f := field{definition: def}
		if def.IsEnum {
			f.enum = make(map[string]struct{}, len(def.EnumValues))
			for _, value := range def.EnumValues {
				f.enum[value] = struct{}{}
			}
		}
		s.fields[name] = f
		s.ordered = append(s.ordered, name)
	}
	sort.Strings(s.ordered)
	v.builds.Add(1)
	for k, item := range v.entries {
		if !now.Before(item.expires) {
			delete(v.entries, k)
		}
	}
	if len(v.entries) >= v.capacity {
		var oldest cacheKey
		minimum := ^uint64(0)
		for k, item := range v.entries {
			if item.used < minimum {
				oldest, minimum = k, item.used
			}
		}
		delete(v.entries, oldest)
	}
	v.entries[key] = entry{s, now.Add(v.ttl), v.sequence}
	return s, nil
}

func (v *Validator) Validate(tenant, objectType, objectID string, values map[string]any, model ports.TenantModel, stored bool) (map[string]any, error) {
	s, err := v.compiled(tenant, objectType, model)
	if err != nil {
		return nil, err
	}
	category := "payload_validation_failed"
	if stored {
		category = "stored_record_invalid"
	}
	failure := &Error{Category: category, ModelRevision: model.RevisionID}
	add := func(name, code, message string) {
		issue := Issue{name, code, message}
		index := sort.Search(len(failure.Issues), func(i int) bool {
			current := failure.Issues[i]
			return current.Field > name || current.Field == name && current.Code >= code
		})
		if len(failure.Issues) == MaxIssues {
			failure.Truncated = true
			if index == MaxIssues {
				return
			}
			failure.Issues = failure.Issues[:MaxIssues-1]
		}
		failure.Issues = append(failure.Issues, Issue{})
		copy(failure.Issues[index+1:], failure.Issues[index:])
		failure.Issues[index] = issue
	}
	if strings.TrimSpace(objectID) == "" {
		add("object_id", "missing_required", "object ID is required")
	}
	out := make(map[string]any, len(s.fields)+1)
	out[s.lookup] = objectID
	if id, supplied := values[s.lookup]; supplied {
		text, ok := id.(string)
		if !ok || strings.TrimSpace(text) != objectID {
			add(s.lookup, "identity_mismatch", "payload identity must match object ID")
		}
	}
	// Inspect all keys; never silently discard an unknown value, including NULL.
	for name, value := range values {
		if name == s.lookup {
			continue
		}
		f, known := s.fields[name]
		_, managed := s.managed[name]
		if !known {
			if name != "id" && name != "updated_at" && name != "valid_from" && name != "valid_until" {
				managed = false
			}
			if !managed {
				add(name, "unknown_field", "field is not active in the object model")
				continue
			}
			// Physical metadata has a fixed platform contract and is optional for evaluation.
			def := ports.TenantModelField{Type: "timestamp", Nullable: name == "valid_until"}
			if name == "id" {
				def.Type = "string"
			}
			f = field{definition: def}
		}
		if value == nil {
			if !f.definition.Nullable {
				add(name, "null_not_allowed", "field does not allow NULL")
				continue
			}
			out[name] = nil
			continue
		}
		normalized, valid := normalize(f.definition.Type, value)
		if !valid {
			add(name, "invalid_type", "value does not satisfy field type "+f.definition.Type)
			continue
		}
		if f.enum != nil {
			if _, ok := f.enum[fmt.Sprint(normalized)]; !ok {
				add(name, "invalid_enum", "value is not in the field enum catalog")
				continue
			}
		}
		out[name] = normalized
	}
	for _, name := range s.ordered {
		if name == s.lookup {
			continue
		}
		if _, managed := s.managed[name]; managed {
			continue
		}
		if _, supplied := values[name]; supplied {
			continue
		}
		if !s.fields[name].definition.Nullable {
			add(name, "missing_required", "field is required for a full evaluation object")
		} else {
			out[name] = nil
		}
	}
	if len(failure.Issues) > 0 {
		sort.Slice(failure.Issues, func(i, j int) bool {
			if failure.Issues[i].Field == failure.Issues[j].Field {
				return failure.Issues[i].Code < failure.Issues[j].Code
			}
			return failure.Issues[i].Field < failure.Issues[j].Field
		})
		if len(failure.Issues) > MaxIssues {
			failure.Issues = failure.Issues[:MaxIssues]
			failure.Truncated = true
		}
		return nil, failure
	}
	return out, nil
}

func normalize(kind string, value any) (any, bool) {
	switch kind {
	case "string":
		text, ok := value.(string)
		return text, ok
	case "bool":
		if b, ok := value.(bool); ok {
			return b, true
		}
		if text, ok := value.(string); ok {
			switch strings.ToLower(strings.TrimSpace(text)) {
			case "true":
				return true, true
			case "false":
				return false, true
			}
		}
	case "int":
		switch n := value.(type) {
		case int:
			return int64(n), true
		case int64:
			return n, true
		case json.Number:
			i, err := n.Int64()
			return i, err == nil
		case string:
			i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
			return i, err == nil
		case float64:
			if !math.IsNaN(n) && !math.IsInf(n, 0) && math.Trunc(n) == n && n >= -9007199254740991 && n <= 9007199254740991 {
				return int64(n), true
			}
		}
	case "float":
		var n float64
		var err error
		switch x := value.(type) {
		case float64:
			n = x
		case float32:
			n = float64(x)
		case int:
			n = float64(x)
		case int64:
			n = float64(x)
		case json.Number:
			n, err = x.Float64()
		case string:
			n, err = strconv.ParseFloat(strings.TrimSpace(x), 64)
		default:
			return nil, false
		}
		return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
	case "timestamp":
		if stamp, ok := value.(time.Time); ok {
			return stamp.UTC(), true
		}
		if text, ok := value.(string); ok {
			stamp, err := time.Parse(time.RFC3339Nano, text)
			return stamp.UTC(), err == nil
		}
	case "ip_address":
		if text, ok := value.(string); ok {
			text = strings.TrimSpace(text)
			_, err := netip.ParseAddr(text)
			return text, err == nil
		}
	}
	return nil, false
}
