package app

import (
	"errors"
	"fmt"
	"strings"
)

var ErrMySQLActivationBlocked = errors.New("MySQL decision runtime activation is blocked pending real owner providers and release approval")

// The target composition can be built/tested explicitly, but changing a
// database URL or backend flag must never run legacy PostgreSQL wiring against
// MySQL or silently activate unverified providers. Empty preserves old callers.
func (c Config) ValidateRuntimeBackend() error {
	switch c.DatabaseBackend {
	case "", "postgres":
		if strings.HasPrefix(strings.ToLower(c.DatabaseURL), "mysql:") {
			return errors.New("MySQL URL cannot use PostgreSQL runtime wiring")
		}
		return nil
	case "mysql":
		return ErrMySQLActivationBlocked
	default:
		return fmt.Errorf("unsupported DATABASE_BACKEND; expected postgres or mysql")
	}
}
