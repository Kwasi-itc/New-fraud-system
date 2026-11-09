// Package sessioncredential is the shared opaque-session credential format.
// Both configured PostgreSQL and staged MySQL verifiers use the same digest.
package sessioncredential

import (
	"crypto/sha256"
	"encoding/base64"
)

func Valid(raw string) bool {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	return err == nil && len(b) == 32
}
func Digest(raw string) []byte { h := sha256.Sum256([]byte(raw)); return h[:] }
