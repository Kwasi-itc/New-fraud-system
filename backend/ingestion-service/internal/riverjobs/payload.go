package riverjobs

import (
	"errors"

	"github.com/google/uuid"
)

var errPayloadContract = errors.New("unsupported or invalid ingestion delivery payload")
var errRunnerUnavailable = errors.New("ingestion worker runner is not configured")

// Schema must agree across migration, producer, worker and readiness checks.
const Schema = "public"

// Version zero is the existing resource-only payload. Version one adds a retry
// identity; the actual domain attempt counter remains authoritative in SQL.
// No translation may infer historical attempts merely from River's Attempt.
func validateDeliveryPayload(id uuid.UUID, version, retryAttempt int) error {
	if id == uuid.Nil || retryAttempt < 0 || retryAttempt > 2147483647 || (version != 0 && version != 1) || (version == 0 && retryAttempt != 0) {
		return errPayloadContract
	}
	return nil
}
