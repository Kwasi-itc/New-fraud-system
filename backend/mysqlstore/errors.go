package mysqlstore

import (
	"errors"

	"github.com/go-sql-driver/mysql"
)

type ErrorKind string

const (
	Other          ErrorKind = "other"
	Duplicate      ErrorKind = "duplicate"
	ForeignKey     ErrorKind = "foreign_key"
	CheckViolation ErrorKind = "check_violation"
	Deadlock       ErrorKind = "deadlock"
	LockTimeout    ErrorKind = "lock_timeout"
)

// Classify is a mechanical category, not an HTTP/domain mapping. In particular,
// a duplicate must be interpreted using the repository's actual unique keys.
func Classify(err error) ErrorKind {
	var commit *CommitError
	if errors.As(err, &commit) {
		return Other
	}
	var rollback *RollbackError
	if errors.As(err, &rollback) {
		return Other
	}
	var cleanup *DDLLockCleanupError
	if errors.As(err, &cleanup) {
		return Other
	}
	var e *mysql.MySQLError
	if !errors.As(err, &e) {
		return Other
	}
	switch e.Number {
	case 1062:
		return Duplicate
	case 1451, 1452:
		return ForeignKey
	case 3819:
		return CheckViolation
	case 1213:
		return Deadlock
	case 1205:
		return LockTimeout
	default:
		return Other
	}
}
