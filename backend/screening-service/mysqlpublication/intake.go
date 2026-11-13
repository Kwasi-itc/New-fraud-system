package mysqlpublication

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/google/uuid"
)

// IntakeRequest is the screening-owned projection of decision dispatch v1.
// Authority comes from the Store's injected service identity, never this body.
type IntakeRequest struct {
	Key, PayloadSHA256               string
	TenantID, ExecutionID            uuid.UUID
	DomainAttempt, RedriveGeneration int
	Payload                          json.RawMessage
}
type IntakeReceipt struct {
	Key, PayloadSHA256 string
	ScreeningID        uuid.UUID
	Found              bool
}
type Intake struct {
	owner     *Store
	providers map[string]struct{}
}

// NewIntake binds a local owner and a fixed provider-name allowlist. It accepts
// no caller-controlled URL and performs no network calls. Accepted means a
// durable pending screening, not a completed external search.
func NewIntake(owner *Store, providers []string) (*Intake, error) {
	if owner == nil || len(providers) == 0 || len(providers) > 100 {
		return nil, ErrInvalid
	}
	allowed := make(map[string]struct{}, len(providers))
	for _, p := range providers {
		if strings.TrimSpace(p) != p || p == "" || len(p) > 128 {
			return nil, ErrInvalid
		}
		allowed[p] = struct{}{}
	}
	return &Intake{owner, allowed}, nil
}

func (r IntakeRequest) validate() ([]byte, error) {
	if r.TenantID == uuid.Nil || r.ExecutionID == uuid.Nil || r.DomainAttempt < 1 || r.DomainAttempt > math.MaxInt32 || r.RedriveGeneration < 0 || r.RedriveGeneration > math.MaxInt32 || len(r.Payload) == 0 || len(r.Payload) > 1<<20 || !json.Valid(r.Payload) {
		return nil, ErrInvalid
	}
	// This encoding is the published decision-v1 reservation key. SQL identity
	// remains the full scalar tuple, so a hash is never the only deduplication key.
	keyJSON, _ := json.Marshal(struct {
		Kind                             string
		TenantID, ResourceID             uuid.UUID
		DomainAttempt, RedriveGeneration int
	}{"screening_execution", r.TenantID, r.ExecutionID, r.DomainAttempt, r.RedriveGeneration})
	keyHash := sha256.Sum256(keyJSON)
	hash := sha256.Sum256(r.Payload)
	if r.Key != "decision-v1-"+hex.EncodeToString(keyHash[:]) || r.PayloadSHA256 != hex.EncodeToString(hash[:]) {
		return nil, ErrInvalid
	}
	return hash[:], nil
}
func strictJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return ErrInvalid
	}
	var tail any
	if err := d.Decode(&tail); err != io.EOF {
		return ErrInvalid
	}
	return nil
}
func (s *Intake) lookup(ctx context.Context, tx *sql.Tx, r IntakeRequest) (IntakeReceipt, error) {
	result := IntakeReceipt{Key: r.Key, PayloadSHA256: r.PayloadSHA256}
	var key string
	var hash []byte
	var id mysqlstore.UUID
	err := tx.QueryRowContext(ctx, "SELECT provider_key,payload_sha256,screening_id FROM "+s.owner.schema+".screening_intake_receipts WHERE tenant_id=? AND execution_id=? AND domain_attempt=? AND redrive_generation=?", mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.ExecutionID), r.DomainAttempt, r.RedriveGeneration).Scan(&key, &hash, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return IntakeReceipt{}, err
	}
	if key != r.Key || hex.EncodeToString(hash) != r.PayloadSHA256 {
		return IntakeReceipt{}, ErrConflict
	}
	result.ScreeningID = uuid.UUID(id)
	result.Found = true
	return result, nil
}

// Reconcile is a read-only indexed receipt lookup. Missing is definitive for
// this exact attempt; racing Deliver calls still serialize on the same key.
func (s *Intake) Reconcile(ctx context.Context, r IntakeRequest) (out IntakeReceipt, err error) {
	if s == nil || s.owner == nil || r.TenantID != s.owner.tenant {
		return out, ErrInvalid
	}
	if _, err = r.validate(); err != nil {
		return out, err
	}
	err = mysqlstore.Snapshot(ctx, s.owner.db, func(tx *sql.Tx) error {
		if err := s.owner.authorize(ctx, tx, r.TenantID, false); err != nil {
			return err
		}
		var err error
		out, err = s.lookup(ctx, tx, r)
		return err
	})
	if err != nil {
		return IntakeReceipt{}, err
	}
	return out, nil
}

// Deliver commits source, projection job and immutable acceptance receipt in
// one short transaction. A commit error exposes no provisional acceptance;
// callers reconcile the original identity. Different attempts of an already
// accepted execution retain the same source and never restart its search.
func (s *Intake) Deliver(ctx context.Context, r IntakeRequest) (out IntakeReceipt, err error) {
	if s == nil || s.owner == nil || r.TenantID != s.owner.tenant {
		return out, ErrInvalid
	}
	hash, err := r.validate()
	if err != nil {
		return out, err
	}
	var body struct {
		ExecutionID string          `json:"execution_id"`
		Request     json.RawMessage `json:"request"`
	}
	if strictJSON(r.Payload, &body) != nil || body.ExecutionID != r.ExecutionID.String() {
		return out, ErrInvalid
	}
	var request screening.SearchRequest
	if strictJSON(body.Request, &request) != nil {
		return out, ErrInvalid
	}
	decision, e1 := uuid.Parse(request.DecisionID)
	scenario, e2 := uuid.Parse(request.ScenarioID)
	config, e3 := uuid.Parse(request.ScreeningConfigID)
	if e1 != nil || e2 != nil || e3 != nil || decision == uuid.Nil || scenario == uuid.Nil || config == uuid.Nil || request.IsManual || request.IdempotencyKey != r.ExecutionID.String() || len(request.Queries) < 1 || len(request.Queries) > 500 {
		return out, ErrInvalid
	}
	if _, ok := s.providers[request.Provider]; !ok {
		return out, ErrInvalid
	}
	if request.InlineResult != nil && request.InlineResult.Validate(request) != nil {
		return out, ErrInvalid
	}
	if request.UniqueCounterpartyIdentifier != nil && (!utf8.ValidString(*request.UniqueCounterpartyIdentifier) || len(*request.UniqueCounterpartyIdentifier) > 512) {
		return out, ErrInvalid
	}
	if !utf8.ValidString(request.ObjectType) || !utf8.ValidString(request.ObjectID) || utf8.RuneCountInString(request.ObjectType) > 128 || utf8.RuneCountInString(request.ObjectID) > 512 {
		return out, ErrInvalid
	}
	item := screening.Screening{TenantID: r.TenantID.String(), Provider: request.Provider, ObjectType: request.ObjectType, ObjectID: request.ObjectID, Status: screening.StatusPending}
	if item.Validate() != nil {
		return out, ErrInvalid
	}
	err = mysqlstore.Mutation(ctx, s.owner.db, func(tx *sql.Tx) error {
		if err := s.owner.authorize(ctx, tx, r.TenantID, true); err != nil {
			return err
		}
		// No-op upsert acquires a single execution reservation, without a shared
		// tenant/parent write lock or an early lock on the decision effect owner.
		_, err := tx.ExecContext(ctx, "INSERT INTO "+s.owner.schema+".screening_intake_keys(tenant_id,execution_id,payload_sha256) VALUES(?,?,?) ON DUPLICATE KEY UPDATE execution_id=execution_id", mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.ExecutionID), hash)
		if err != nil {
			return err
		}
		var storedHash []byte
		if err = tx.QueryRowContext(ctx, "SELECT payload_sha256 FROM "+s.owner.schema+".screening_intake_keys WHERE tenant_id=? AND execution_id=? FOR UPDATE", mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.ExecutionID)).Scan(&storedHash); err != nil {
			return err
		}
		if !bytes.Equal(storedHash, hash) {
			return ErrConflict
		}
		prior, err := s.lookup(ctx, tx, r)
		if err != nil {
			return err
		}
		if prior.Found {
			out = prior
			return nil
		}
		var source mysqlstore.UUID
		err = tx.QueryRowContext(ctx, "SELECT id FROM "+s.owner.schema+".screening_screenings WHERE tenant_id=? AND execution_id=? FOR UPDATE", mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.ExecutionID)).Scan(&source)
		if errors.Is(err, sql.ErrNoRows) {
			source = mysqlstore.UUID(uuid.New())
			_, err = tx.ExecContext(ctx, "INSERT INTO "+s.owner.schema+`.screening_screenings(id,tenant_id,decision_id,scenario_id,screening_config_id,execution_id,status,provider,object_type,object_id,request_json,unique_counterparty_identifier,response_json,decision_projection,provider_reference,last_error,created_at,updated_at)
   VALUES(?,?,?,?,?,?,'pending',?,?,?,?,?,'{}','{}','','',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, source, mysqlstore.UUID(r.TenantID), mysqlstore.UUID(decision), mysqlstore.UUID(scenario), mysqlstore.UUID(config), mysqlstore.UUID(r.ExecutionID), request.Provider, request.ObjectType, request.ObjectID, mysqlstore.JSON(body.Request), request.UniqueCounterpartyIdentifier)
		} else if err == nil {
			// Only this intake may replay a pre-existing source; old fixture/import
			// rows without a receipt must not be silently adopted as accepted work.
			var receipts int
			err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM (SELECT 1 FROM "+s.owner.schema+".screening_intake_receipts WHERE tenant_id=? AND execution_id=? LIMIT 1) r", mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.ExecutionID)).Scan(&receipts)
			if err == nil && receipts != 1 {
				return ErrConflict
			}
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO "+s.owner.schema+".screening_intake_receipts(tenant_id,execution_id,domain_attempt,redrive_generation,provider_key,payload_sha256,screening_id,accepted_at) VALUES(?,?,?,?,?,?,?,UTC_TIMESTAMP(6))", mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.ExecutionID), r.DomainAttempt, r.RedriveGeneration, r.Key, hash, source)
		if err == nil {
			out = IntakeReceipt{r.Key, r.PayloadSHA256, uuid.UUID(source), true}
		}
		return err
	})
	if err != nil {
		return IntakeReceipt{}, err
	}
	return out, nil
}
