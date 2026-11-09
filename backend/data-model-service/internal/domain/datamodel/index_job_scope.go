package datamodel

import (
	"errors"
	"github.com/google/uuid"
)

var ErrIndexJobScopeLimit = errors.New("index job scope exceeds bounded result limit")
var ErrIndexJobRetryConflict = errors.New("index job or delivery is not eligible for retry")

// ValidateIndexJobScope bounds both SQL parameters and materialized results.
// Overflow is an error; a partial job set cannot establish preparation readiness.
func ValidateIndexJobScope(tenant uuid.UUID, tables []uuid.UUID, limit int) error {
	if tenant == uuid.Nil || len(tables) == 0 || len(tables) > 100 || limit < 1 || limit > 1000 {
		return ErrIndexJobScopeLimit
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range tables {
		if id == uuid.Nil || seen[id] {
			return ErrIndexJobScopeLimit
		}
		seen[id] = true
	}
	return nil
}
