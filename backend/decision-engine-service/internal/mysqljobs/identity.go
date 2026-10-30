// Package mysqljobs defines the staged MySQL decision transport contract. It
// does not start workers, authorize effects or replace the River runtime.
package mysqljobs

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

var ErrIdentity = errors.New("invalid or unsupported MySQL decision delivery identity")

// Identity carries the exact reservation/receipt tuple, NOT a digest of serialized
// JSON. DomainAttempt is positive and independent of a transport lease attempt.
// Callback attempts have their OWN domain counter, not the evaluation counter.
// RedriveGeneration starts at zero and changes only under explicit authorized
// owner redrive. A stale delivery cannot infer or allocate a newer generation.
//
// Only an owner adapter which has locked/rechecked the same-tenant resource may
// construct an accepted identity. Decoding these fields is never authorization.
// New domain attempts/redrives are allocated atomically with owner state/jobs;
// transport retry/rescue keeps the complete Identity unchanged. Resource deletion
// and receipt retention must be coordinated before any queue cleanup is enabled.
type Identity struct {
	Version           int           `json:"payload_version"`
	Kind              ports.JobKind `json:"kind"`
	TenantID          uuid.UUID     `json:"tenant_id"`
	ResourceID        uuid.UUID     `json:"resource_id"`
	DomainAttempt     int           `json:"domain_attempt"`
	RedriveGeneration int           `json:"redrive_generation"`
}

// ReservationKey excludes the payload encoding version and queue configuration.
// Neither may create a second logical delivery for the same owner attempt.
// Persistence must use these complete scalar columns, not a unique hash alone.
type ReservationKey struct {
	Kind                             ports.JobKind
	TenantID, ResourceID             uuid.UUID
	DomainAttempt, RedriveGeneration int
}

func (v Identity) Key() (ReservationKey, error) {
	if err := v.Validate(); err != nil {
		return ReservationKey{}, err
	}
	return ReservationKey{v.Kind, v.TenantID, v.ResourceID, v.DomainAttempt, v.RedriveGeneration}, nil
}

// Contract returns a copy of the shared source catalog entry, not a configured
// queue/handler. Queue overrides, budgets, readiness and worker scope are separate.
func (v Identity) Contract() (workers.LegacyContract, error) {
	for _, entry := range workers.LegacyCatalog() {
		if entry.Owner == "decision_engine" && entry.Kind == string(v.Kind) && !entry.Global {
			return entry, nil
		}
	}
	return workers.LegacyContract{}, ErrIdentity
}

func (v Identity) Validate() error {
	if v.Version != 1 || v.TenantID == uuid.Nil || v.ResourceID == uuid.Nil || v.DomainAttempt < 1 || v.DomainAttempt > math.MaxInt32 || v.RedriveGeneration < 0 || v.RedriveGeneration > math.MaxInt32 {
		return ErrIdentity
	}
	_, err := v.Contract()
	return err
}

func Encode(v Identity) ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// Decode accepts only this explicit target v1 format. It never auto-upgrades a
// legacy River payload or derives a missing tenant/attempt from defaults. Unknown
// fields, duplicate fields, NULLs, fractional/overflow integers, noncanonical
// identities and trailing JSON are rejected before an owner query is attempted.
func Decode(raw []byte) (Identity, error) {
	if len(raw) == 0 || len(raw) > 4096 {
		return Identity{}, ErrIdentity
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return Identity{}, ErrIdentity
	}
	var out Identity
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return Identity{}, ErrIdentity
		}
		name, ok := token.(string)
		if !ok || seen[name] {
			return Identity{}, ErrIdentity
		}
		seen[name] = true
		switch name {
		case "payload_version", "domain_attempt", "redrive_generation":
			var value *int
			if err := d.Decode(&value); err != nil || value == nil {
				return Identity{}, ErrIdentity
			}
			switch name {
			case "payload_version":
				out.Version = *value
			case "domain_attempt":
				out.DomainAttempt = *value
			default:
				out.RedriveGeneration = *value
			}
		case "kind", "tenant_id", "resource_id":
			var value *string
			if err := d.Decode(&value); err != nil || value == nil {
				return Identity{}, ErrIdentity
			}
			if name == "kind" {
				out.Kind = ports.JobKind(*value)
				continue
			}
			id, err := uuid.Parse(*value)
			if err != nil || id == uuid.Nil || id.String() != *value {
				return Identity{}, ErrIdentity
			}
			if name == "tenant_id" {
				out.TenantID = id
			} else {
				out.ResourceID = id
			}
		default:
			return Identity{}, ErrIdentity
		}
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return Identity{}, ErrIdentity
	}
	if _, err := d.Token(); err != io.EOF {
		return Identity{}, ErrIdentity
	}
	if len(seen) != 6 || out.Validate() != nil {
		return Identity{}, ErrIdentity
	}
	return out, nil
}
