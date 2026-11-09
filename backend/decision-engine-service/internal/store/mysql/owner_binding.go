package mysql

import (
	"database/sql"
	"github.com/google/uuid"
	"strings"
)

// MatchesOwner verifies a separately owned SQL capability without exposing the
// request pool to HTTP handlers or letting route IDs establish owner authority.
func (s *ExecutionStore) MatchesOwner(owner interface {
	Matches(*sql.DB, string, uuid.UUID) bool
}) bool {
	return s != nil && s.configuration != nil && owner != nil && owner.Matches(s.configuration.db, strings.Trim(s.configuration.schema, "`"), s.configuration.tenant)
}
