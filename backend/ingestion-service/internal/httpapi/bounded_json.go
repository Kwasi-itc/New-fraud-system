package httpapi

import "encoding/json"

// validBoundedJSON rejects multiple documents and bounds nesting before handler
// metrics/validation traverse a decoded tree. The caller already bounds bytes.
// Counting delimiters after syntax validation is linear and allocates no tree.
func validBoundedJSON(data []byte, maxDepth int) bool {
	if maxDepth < 1 || !json.Valid(data) {
		return false
	}
	depth := 0
	inString, escaped := false, false
	for _, b := range data {
		if inString {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > maxDepth {
				return false
			}
		case '}', ']':
			depth--
		}
	}
	return true
}
