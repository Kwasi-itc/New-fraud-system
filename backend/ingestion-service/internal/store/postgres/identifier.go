package postgres

import (
	"strings"
)

func sanitizeIdentifier(parts ...string) string {
	quoted := make([]string, len(parts))
	for i, part := range parts {
		escaped := strings.ReplaceAll(part, `"`, `""`)
		quoted[i] = `"` + escaped + `"`
	}
	return strings.Join(quoted, ".")
}
