package ingestion

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/google/uuid"
)

const ReceiptCommandVersion = "ingestion-request/v1"

// ReceiptCommand describes raw decoded input before mutable model validation.
// Tenant and stable table identity must be resolved by the trusted owner.
// Authorization is separate and must run on every execution and replay.
type ReceiptCommand struct {
	TenantID uuid.UUID
	TableID  uuid.UUID
	Mode     Mode
	Kind     string
	Records  []map[string]any
}

// CanonicalReceiptCommand binds scope and input using a pinned JSON envelope.
// Object keys sort lexically; array/record order and null/absent distinctions
// remain significant. Numeric spellings in json.Number remain significant.
// No model defaults, coercions, timestamps or publication versions are applied.
// Limits are checked before marshaling, including recursive byte/node/depth
// budgets, so cyclic or large decoded inputs fail without unbounded traversal.
func CanonicalReceiptCommand(command ReceiptCommand, maxBytes int) ([]byte, [32]byte, error) {
	var zero [32]byte
	if maxBytes < 1 || maxBytes > 16<<20 || command.TenantID == uuid.Nil || command.TableID == uuid.Nil || (command.Mode != ModeCreate && command.Mode != ModePatch) || (command.Kind != "single" && command.Kind != "batch") || len(command.Records) < 1 || len(command.Records) > 500 || (command.Kind == "single" && len(command.Records) != 1) {
		return nil, zero, errors.New("bounded scoped receipt command required")
	}
	b := commandBudget{bytes: maxBytes, nodes: maxBytes}
	for _, record := range command.Records {
		if record == nil {
			return nil, zero, errors.New("receipt record must be an object")
		}
		if err := b.visit(record, 0); err != nil {
			return nil, zero, err
		}
	}
	// Field order and names are part of v1, not a generic JSON canonicalization.
	envelope := struct {
		Version string           `json:"version"`
		Tenant  uuid.UUID        `json:"tenant"`
		Table   uuid.UUID        `json:"table"`
		Mode    Mode             `json:"mode"`
		Kind    string           `json:"kind"`
		Records []map[string]any `json:"records"`
	}{ReceiptCommandVersion, command.TenantID, command.TableID, command.Mode, command.Kind, command.Records}
	wire, err := json.Marshal(envelope)
	if err != nil {
		return nil, zero, fmt.Errorf("encode receipt command: %w", err)
	}
	if len(wire) > maxBytes {
		return nil, zero, errors.New("receipt command byte limit exceeded")
	}
	return wire, sha256.Sum256(wire), nil
}

type commandBudget struct{ bytes, nodes int }

func (b *commandBudget) text(s string) error {
	if !utf8.ValidString(s) {
		return errors.New("receipt command contains invalid UTF-8")
	}
	b.bytes -= len(s)
	if b.bytes < 0 {
		return errors.New("receipt command byte limit exceeded")
	}
	return nil
}

func (b *commandBudget) visit(value any, depth int) error {
	b.nodes--
	if depth > 32 || b.nodes < 0 {
		return errors.New("receipt command structural limit exceeded")
	}
	switch v := value.(type) {
	case nil, bool:
		return nil
	case string:
		return b.text(v)
	case json.Number:
		if err := b.text(string(v)); err != nil {
			return err
		}
		if len(v) == 0 || !json.Valid([]byte(v)) {
			return errors.New("invalid receipt number")
		}
		// Marshal rejects non-JSON numeric spellings without converting to float.
		_, err := json.Marshal(v)
		return err
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("nonfinite receipt number")
		}
		return nil
	case map[string]any:
		if len(v) > b.nodes {
			return errors.New("receipt command structural limit exceeded")
		}
		for key, child := range v {
			if err := b.text(key); err != nil {
				return err
			}
			if err := b.visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	case []any:
		if len(v) > b.nodes {
			return errors.New("receipt command structural limit exceeded")
		}
		for _, child := range v {
			if err := b.visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported decoded receipt value %T", value)
	}
}
