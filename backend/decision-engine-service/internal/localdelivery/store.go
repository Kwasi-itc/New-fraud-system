package localdelivery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

const BaseURL = "http://decision-local-providers:8090"
const CallbackURL = BaseURL + "/callbacks"
const EventURL = BaseURL + "/events"
const MaxPayloadBytes = 4 << 20

var ErrRequest = errors.New("invalid or unsupported local delivery request")
var ErrConflict = errors.New("local delivery identity conflicts with retained payload")

type Authorize func(context.Context, *sql.Tx, uuid.UUID, bool) error
type Store struct {
	db               *sql.DB
	database, schema string
	tenant           uuid.UUID
	authorize        Authorize
}

func NewStore(db *sql.DB, database string, tenant uuid.UUID, authorize Authorize) (*Store, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil || authorize == nil {
		return nil, ErrRequest
	}
	return &Store{db, database, s, tenant, authorize}, nil
}
func Validate(r ports.DispatchRequest, tenant uuid.UUID) error {
	t, e1 := uuid.Parse(r.TenantID)
	resource, e2 := uuid.Parse(r.ResourceID)
	if e1 != nil || e2 != nil || t != tenant || t.String() != r.TenantID || resource.String() != r.ResourceID || len(r.Payload) == 0 || len(r.Payload) > MaxPayloadBytes || !json.Valid(r.Payload) {
		return ErrRequest
	}
	id := mysqljobs.Identity{Version: 1, Kind: r.Kind, TenantID: t, ResourceID: resource, DomainAttempt: r.DomainAttempt, RedriveGeneration: r.RedriveGeneration}
	key, err := id.Key()
	if err != nil {
		return ErrRequest
	}
	raw, _ := json.Marshal(key)
	hash := sha256.Sum256(raw)
	payload := sha256.Sum256(r.Payload)
	if r.Key != "decision-v1-"+hex.EncodeToString(hash[:]) || r.PayloadSHA256 != hex.EncodeToString(payload[:]) {
		return ErrRequest
	}
	return nil
}

type Delivery struct {
	Request   ports.DispatchRequest `json:"request"`
	Reference string                `json:"reference"`
	Result    json.RawMessage       `json:"result"`
	Applied   bool                  `json:"applied"`
	Outcome   ports.DispatchOutcome `json:"outcome"`
}

func (s *Store) lookup(ctx context.Context, tx *sql.Tx, r ports.DispatchRequest) (ports.DispatchReceipt, error) {
	out := ports.DispatchReceipt{Key: r.Key, PayloadSHA256: r.PayloadSHA256, Outcome: ports.DispatchNotFound}
	var hash []byte
	var ref mysqlstore.UUID
	err := tx.QueryRowContext(ctx, "SELECT payload_hash,reference,outcome FROM "+s.schema+".decision_local_delivery_receipts WHERE tenant_id=? AND kind=? AND resource_id=? AND domain_attempt=? AND redrive_generation=?", mysqlstore.UUID(s.tenant), []byte(r.Kind), mysqlstore.UUID(uuid.MustParse(r.ResourceID)), r.DomainAttempt, r.RedriveGeneration).Scan(&hash, &ref, &out.Outcome)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if hex.EncodeToString(hash) != r.PayloadSHA256 {
		return out, ErrConflict
	}
	out.Reference = uuid.UUID(ref).String()
	return out, nil
}
func (s *Store) Reconcile(ctx context.Context, r ports.DispatchRequest) (out ports.DispatchReceipt, err error) {
	if err = Validate(r, s.tenant); err != nil {
		return out, err
	}
	err = mysqlstore.Snapshot(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, false); err != nil {
			return err
		}
		out, err = s.lookup(ctx, tx, r)
		return err
	})
	if err != nil {
		return ports.DispatchReceipt{}, err
	}
	return out, err
}
func (s *Store) Record(ctx context.Context, r ports.DispatchRequest, result json.RawMessage) (ports.DispatchReceipt, error) {
	return s.record(ctx, r, result, ports.DispatchAccepted, uuid.New())
}
func (s *Store) Reject(ctx context.Context, r ports.DispatchRequest) (ports.DispatchReceipt, error) {
	return s.record(ctx, r, json.RawMessage(`{"backend":"local-development","rejected":"local_receiver_policy"}`), ports.DispatchRejected, uuid.New())
}
func (s *Store) record(ctx context.Context, r ports.DispatchRequest, result json.RawMessage, outcome ports.DispatchOutcome, reference uuid.UUID) (out ports.DispatchReceipt, err error) {
	if err = Validate(r, s.tenant); err != nil || !json.Valid(result) || len(result) > 1<<20 {
		return out, ErrRequest
	}
	// Payload/result and the durable inbox acceptance are the same transaction.
	err = mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, true); err != nil {
			return err
		}
		hash, _ := hex.DecodeString(r.PayloadSHA256)
		_, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+".decision_local_delivery_receipts(tenant_id,kind,resource_id,domain_attempt,redrive_generation,provider_key,payload_hash,reference,payload,result,outcome,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE provider_key=provider_key", mysqlstore.UUID(s.tenant), []byte(r.Kind), mysqlstore.UUID(uuid.MustParse(r.ResourceID)), r.DomainAttempt, r.RedriveGeneration, r.Key, hash, mysqlstore.UUID(reference), []byte(r.Payload), mysqlstore.JSON(result), string(outcome))
		if err != nil {
			return err
		}
		out, err = s.lookup(ctx, tx, r)
		return err
	})
	if err != nil {
		return ports.DispatchReceipt{}, err
	}
	return out, err
}
func (s *Store) Read(ctx context.Context, kind ports.JobKind, resource uuid.UUID) (out []Delivery, err error) {
	err = mysqlstore.Snapshot(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, false); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT domain_attempt,redrive_generation,provider_key,payload_hash,reference,payload,result,applied_at IS NOT NULL,outcome FROM "+s.schema+".decision_local_delivery_receipts WHERE tenant_id=? AND kind=? AND resource_id=? ORDER BY domain_attempt DESC,redrive_generation DESC LIMIT 10", mysqlstore.UUID(s.tenant), []byte(kind), mysqlstore.UUID(resource))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v := Delivery{Request: ports.DispatchRequest{Kind: kind, TenantID: s.tenant.String(), ResourceID: resource.String()}}
			var hash []byte
			var ref mysqlstore.UUID
			if err := rows.Scan(&v.Request.DomainAttempt, &v.Request.RedriveGeneration, &v.Request.Key, &hash, &ref, &v.Request.Payload, &v.Result, &v.Applied, &v.Outcome); err != nil {
				return err
			}
			v.Request.PayloadSHA256 = hex.EncodeToString(hash)
			v.Reference = uuid.UUID(ref).String()
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}
