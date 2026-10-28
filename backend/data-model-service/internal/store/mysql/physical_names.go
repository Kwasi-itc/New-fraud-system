package mysql

import "strings"

// physicalLabel is a creation-time display hint, not an identity. The generated
// physical name also contains the full immutable UUID. Limit is an ASCII byte
// budget; non-ASCII/punctuation runs become separators, never executable SQL.
func physicalLabel(value string, limit int, fallback string) string {
	var result strings.Builder
	separator := false
	for _, c := range value {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			if separator && result.Len() > 0 && result.Len() < limit {
				result.WriteByte('_')
			}
			if result.Len() == limit {
				break
			}
			result.WriteByte(byte(c))
			separator = false
		} else {
			separator = true
		}
		if result.Len() == limit {
			break
		}
	}
	label := strings.TrimRight(result.String(), "_")
	if label == "" {
		return fallback
	}
	return label
}
