package workers

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// LegacyContract inventories the pinned River transport, not a registry that
// enables work. Transport retries and owner/domain retries are separate. Legacy
// payloads have NO embedded version; their migration input version is zero.
// ArgsRetained includes completed jobs until transport retention removes them;
// ActiveQueue releases its singleton reservation on every terminal outcome.
type UniqueScope string

const (
	ArgsRetained          UniqueScope = "kind_and_args_retained"
	ActiveQueue           UniqueScope = "kind_and_queue_active"
	NoTransportUniqueness UniqueScope = "none_owner_fences_effects"
)

type LegacyContract struct {
	Owner, Kind, Queue string
	ResourceField      string
	TenantInPayload    bool
	Global             bool
	Timeout            time.Duration
	Unique             UniqueScope
}

// LegacyCatalog returns a new value on every call. Queues are the source
// defaults; deployments may override them. Enabling a MySQL handler still needs
// explicit owner schema/readiness, budgets, policies, authorization and effects.
func LegacyCatalog() []LegacyContract {
	return []LegacyContract{
		{"data_model", "index_job_execute", "index_jobs", "index_job_id", false, false, 30 * time.Second, ArgsRetained},
		{"ingestion", "upload_log_execute", "upload_logs", "upload_log_id", false, false, 5 * time.Minute, ArgsRetained},
		{"ingestion", "deferred_ingest_execute", "deferred_ingests", "deferred_ingest_id", false, false, 5 * time.Minute, ArgsRetained},
		{"decision_engine", "scheduled_execution", "scheduled_executions", "execution_id", false, false, 10 * time.Minute, ArgsRetained},
		{"decision_engine", "async_decision_execution", "async_decision_executions", "execution_id", false, false, 10 * time.Minute, ArgsRetained},
		{"decision_engine", "async_decision_execution_callback", "async_decision_execution_callbacks", "execution_id", true, false, 2 * time.Minute, ArgsRetained},
		{"decision_engine", "workflow_execution", "workflow_executions", "execution_id", true, false, 5 * time.Minute, ArgsRetained},
		{"decision_engine", "screening_execution", "screening_executions", "execution_id", true, false, 5 * time.Minute, ArgsRetained},
		{"decision_engine", "scoring_request", "scoring_requests", "request_id", true, false, 5 * time.Minute, ArgsRetained},
		{"decision_engine", "outbox_event", "outbox_events", "event_id", true, false, 5 * time.Minute, ArgsRetained},
		{"screening", "screening_dispatch", "screening_dispatch", "screening_id", true, false, 5 * time.Minute, NoTransportUniqueness},
		{"screening", "dataset_update_job", "dataset_update_jobs", "job_id", true, false, 5 * time.Minute, ArgsRetained},
		{"screening", "monitored_object", "monitored_objects", "monitored_object_id", true, false, 5 * time.Minute, NoTransportUniqueness},
		{"case_manager", "case_maintenance", "case_maintenance", "", false, true, 30 * time.Minute, ActiveQueue},
		{"case_manager", "case_delivery", "case_delivery", "", false, true, 30 * time.Minute, ActiveQueue},
	}
}

var ErrPayloadContract = errors.New("unsupported or invalid legacy work payload")

// LegacyIdentity contains only transport identity. A missing TenantID for an
// owner-resource-only payload MUST be resolved through that owner's authorized
// resource lookup before mutation. It is never filled with a default tenant.
// Global maintenance has neither identity and is explicitly marked Global.
// This does not translate attempts/error history, schedules, reservations or
// receipts, and is not authorization to reenqueue legacy work.
type LegacyIdentity struct {
	Owner, Kind, TenantID, ResourceID string
	Global                            bool
}

func DecodeLegacyIdentity(kind string, version int, payload []byte) (LegacyIdentity, error) {
	if version != 0 {
		return LegacyIdentity{}, ErrPayloadContract
	}
	decoded, err := DecodeIdentity(kind, version, payload)
	return decoded.LegacyIdentity, err
}

// PayloadIdentity records explicit versioned transport identity. A retry number
// is not authority to change owner attempt history; the owner must verify it.
type PayloadIdentity struct {
	LegacyIdentity
	Version      int
	RetryAttempt int
}

func supportsRetryIdentity(kind string) bool {
	return kind == "upload_log_execute" || kind == "deferred_ingest_execute"
}

// DecodeIdentity accepts the catalog's legacy v0 payloads and ingestion's v1
// per-attempt delivery identity. Unknown kind/version/fields fail closed. It
// does not upgrade job state or infer history from transport attempt numbers.
func DecodeIdentity(kind string, version int, payload []byte) (PayloadIdentity, error) {
	var c LegacyContract
	for _, candidate := range LegacyCatalog() {
		if candidate.Kind == kind {
			c = candidate
			break
		}
	}
	versioned := version == 1 && supportsRetryIdentity(kind)
	if (version != 0 && !versioned) || c.Kind == "" || len(payload) == 0 || len(payload) > 4096 {
		return PayloadIdentity{}, ErrPayloadContract
	}
	d := json.NewDecoder(bytes.NewReader(payload))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return PayloadIdentity{}, ErrPayloadContract
	}
	values := map[string]string{}
	seen := map[string]bool{}
	retryAttempt := 0
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return PayloadIdentity{}, ErrPayloadContract
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return PayloadIdentity{}, ErrPayloadContract
		}
		seen[name] = true
		if versioned && (name == "payload_version" || name == "retry_attempt") {
			var value int
			if err := d.Decode(&value); err != nil || value < 0 || value > 2147483647 {
				return PayloadIdentity{}, ErrPayloadContract
			}
			if name == "payload_version" {
				if value != version {
					return PayloadIdentity{}, ErrPayloadContract
				}
			} else {
				retryAttempt = value
			}
			continue
		}
		if !ok || name == "" || (!(name == c.ResourceField && !c.Global) && !(name == "tenant_id" && c.TenantInPayload)) {
			return PayloadIdentity{}, ErrPayloadContract
		}
		var value string
		if err := d.Decode(&value); err != nil || !canonicalUUID(value) {
			return PayloadIdentity{}, ErrPayloadContract
		}
		values[name] = value
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return PayloadIdentity{}, ErrPayloadContract
	}
	if _, err := d.Token(); err != io.EOF {
		return PayloadIdentity{}, ErrPayloadContract
	}
	if !c.Global && values[c.ResourceField] == "" || c.TenantInPayload && values["tenant_id"] == "" || versioned && !seen["payload_version"] {
		return PayloadIdentity{}, ErrPayloadContract
	}
	return PayloadIdentity{LegacyIdentity: LegacyIdentity{c.Owner, c.Kind, values["tenant_id"], values[c.ResourceField], c.Global}, Version: version, RetryAttempt: retryAttempt}, nil
}

func canonicalUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	var compact [32]byte
	n := 0
	for i := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		c := value[i]
		if c >= 'A' && c <= 'F' {
			return false
		}
		compact[n] = c
		n++
	}
	var binary [16]byte
	if _, err := hex.Decode(binary[:], compact[:]); err != nil {
		return false
	}
	return binary != [16]byte{}
}
