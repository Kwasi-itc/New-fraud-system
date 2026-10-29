package mysqlstore

import (
	"errors"
	"fmt"
)

// PoolBudget describes a configured pool, not a measured capacity claim. Name
// is a fixed deployment label, never a DSN, database password or tenant value.
// Include API/read/worker replicas, installers, DDL, monitoring and providers.
type PoolBudget struct {
	Name     string
	Replicas int64
	MaxOpen  int64
}

type ConnectionBudget struct {
	ServerLimit       int64 `json:"server_limit"`
	Reserved          int64 `json:"reserved"`
	ConfiguredMaximum int64 `json:"configured_maximum"`
	Unallocated       int64 `json:"unallocated"`
}

// ValidateConnectionBudget fails closed for missing/unbounded/duplicate entries
// and overflow. It does not discover omitted pools or infer useful concurrency.
func ValidateConnectionBudget(serverLimit, reserved int64, pools []PoolBudget) (ConnectionBudget, error) {
	if serverLimit <= 0 || reserved < 0 || reserved >= serverLimit || len(pools) == 0 || len(pools) > 1024 {
		return ConnectionBudget{}, errors.New("positive server limit, reserve headroom and bounded pool inventory required")
	}
	result := ConnectionBudget{ServerLimit: serverLimit, Reserved: reserved, Unallocated: serverLimit - reserved}
	seen := make(map[string]bool, len(pools))
	for _, pool := range pools {
		if !identifier.MatchString(pool.Name) || seen[pool.Name] || pool.Replicas <= 0 || pool.MaxOpen <= 0 {
			return ConnectionBudget{}, errors.New("unique pool labels and positive replica/pool limits required")
		}
		seen[pool.Name] = true
		// Division checks capacity before multiplication, avoiding integer overflow.
		if pool.Replicas > result.Unallocated/pool.MaxOpen {
			return ConnectionBudget{}, fmt.Errorf("configured pools exceed database connection budget at %s", pool.Name)
		}
		connections := pool.Replicas * pool.MaxOpen
		result.ConfiguredMaximum += connections
		result.Unallocated -= connections
	}
	return result, nil
}
