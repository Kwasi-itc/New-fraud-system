package ports

import (
	"context"
	"encoding/json"
)

// DispatchRequest is the v1 decision-to-provider contract. The key identifies
// kind + tenant + resource + domain attempt + explicit redrive generation, never
// the transport retry number. A provider MUST atomically deduplicate that key
// and reject reuse with a different body hash. It must retain its receipt for
// at least the decision owner's lifetime, independent of queue retention.
type DispatchRequest struct {
	Key               string          `json:"idempotency_key"`
	Kind              JobKind         `json:"kind"`
	TenantID          string          `json:"tenant_id"`
	ResourceID        string          `json:"resource_id"`
	DomainAttempt     int             `json:"domain_attempt"`
	RedriveGeneration int             `json:"redrive_generation"`
	PayloadSHA256     string          `json:"payload_sha256"`
	Payload           json.RawMessage `json:"payload"`
}

type DispatchOutcome string

const (
	DispatchAccepted DispatchOutcome = "accepted"
	DispatchRejected DispatchOutcome = "rejected"
	DispatchUnknown  DispatchOutcome = "unknown"
	// NotFound is definitive absence only at an atomic, idempotent provider.
	// Deliver can safely race an earlier in-flight Deliver using the SAME key.
	DispatchNotFound DispatchOutcome = "not_found"
)

type DispatchReceipt struct {
	Key           string          `json:"idempotency_key"`
	PayloadSHA256 string          `json:"payload_sha256"`
	Outcome       DispatchOutcome `json:"outcome"`
	Reference     string          `json:"reference"`
}

// ReceiptDispatcher performs network IO outside SQL transactions. It must
// return echoed key/hash, a nonempty bounded opaque receipt for terminal
// outcomes, obey context, enforce destination/credential policy and disable
// unsafe redirects. Timeouts/non-2xx alone are NOT proof of rejection/absence.
// A legacy HTTP 2xx sender without reconcile support cannot satisfy this port.
// These are provider obligations, not an exactly-once external-effect claim.
type ReceiptDispatcher interface {
	Deliver(context.Context, DispatchRequest) (DispatchReceipt, error)
	Reconcile(context.Context, DispatchRequest) (DispatchReceipt, error)
}
