package mysql

import "errors"

// ValidateLegacyEvidence checks the same source evidence used by Import, without
// accessing a database. Success is not proof of authenticity, current permission,
// absence of target conflicts, or completeness of a source inventory. Diagnostics
// deliberately exclude decoder errors which may contain source field values.
func ValidateLegacyEvidence(in LegacyImport, maxBytes int) error {
	_, _, _, err := translateLegacy(in, maxBytes)
	if errors.Is(err, ErrLegacyEvidenceLimit) {
		return ErrLegacyEvidenceLimit
	}
	if err != nil {
		return ErrLegacyRejected
	}
	return nil
}
