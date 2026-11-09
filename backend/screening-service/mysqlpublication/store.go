// Package mysqlpublication owns staged screening status/evidence publications.
// It performs local transactions only; provider calls happen before these APIs.
package mysqlpublication

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrConflict = errors.New("screening publication version or transition conflict")
var ErrInvalid = errors.New("invalid screening publication input")

type Authorize func(context.Context, *sql.Tx, uuid.UUID, bool) error
type Store struct {
	db        *sql.DB
	schema    string
	tenant    uuid.UUID
	authorize Authorize
}
type Match struct {
	ID                               uuid.UUID
	EntityID, Provider, Name, Status string
	Score                            float64
	Payload                          json.RawMessage
}
type Result struct {
	Response  json.RawMessage
	Reference string
	Partial   bool
	Matches   []Match
}

func New(db *sql.DB, database string, tenant uuid.UUID, authorize Authorize) (*Store, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil || authorize == nil {
		return nil, ErrInvalid
	}
	return &Store{db, s, tenant, authorize}, nil
}

// mutate orders authority -> one source row -> bounded matches -> queue trigger.
// An expected version is mandatory; stale writers cannot overwrite a review.
func (s *Store) mutate(ctx context.Context, id uuid.UUID, expected int64, fn func(*sql.Tx, string, bool) error) (int64, error) {
	if s == nil || id == uuid.Nil || expected < 1 || expected == math.MaxInt64 {
		return 0, ErrInvalid
	}
	err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, true); err != nil {
			return err
		}
		var version int64
		var status string
		var partial bool
		if err := tx.QueryRowContext(ctx, "SELECT projection_version,status,partial FROM "+s.schema+".screening_screenings WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(s.tenant), mysqlstore.UUID(id)).Scan(&version, &status, &partial); err != nil {
			return err
		}
		if version != expected {
			return ErrConflict
		}
		return fn(tx, status, partial)
	})
	if err != nil {
		return 0, err
	}
	return expected + 1, nil
}
func (s *Store) advance(ctx context.Context, tx *sql.Tx, id uuid.UUID, assign string, args ...any) error {
	args = append(args, mysqlstore.UUID(s.tenant), mysqlstore.UUID(id))
	_, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".screening_screenings SET "+assign+",projection_version=projection_version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", args...)
	return err
}
func (s *Store) Start(ctx context.Context, id uuid.UUID, version int64) (int64, error) {
	return s.mutate(ctx, id, version, func(tx *sql.Tx, status string, _ bool) error {
		if status != "pending" && status != "queued" {
			return ErrConflict
		}
		return s.advance(ctx, tx, id, "status='processing',sent_at=UTC_TIMESTAMP(6),last_error='',failed_at=NULL")
	})
}
func (s *Store) Fail(ctx context.Context, id uuid.UUID, version int64, message string) (int64, error) {
	if len(message) > 16384 {
		return 0, ErrInvalid
	}
	return s.mutate(ctx, id, version, func(tx *sql.Tx, status string, _ bool) error {
		if status != "processing" && status != "queued" && status != "pending" {
			return ErrConflict
		}
		return s.advance(ctx, tx, id, "status='failed',last_error=?,failed_at=UTC_TIMESTAMP(6)", message)
	})
}
func (s *Store) Retry(ctx context.Context, id uuid.UUID, version int64) (int64, error) {
	return s.mutate(ctx, id, version, func(tx *sql.Tx, status string, _ bool) error {
		if status != "failed" {
			return ErrConflict
		}
		return s.advance(ctx, tx, id, "status='pending',last_error='',failed_at=NULL,completed_at=NULL")
	})
}
func (s *Store) Archive(ctx context.Context, id uuid.UUID, version int64) (int64, error) {
	return s.mutate(ctx, id, version, func(tx *sql.Tx, status string, _ bool) error {
		if status != "confirmed_hit" && status != "no_hit" {
			return ErrConflict
		}
		return s.advance(ctx, tx, id, "status='archived'")
	})
}
func (s *Store) Complete(ctx context.Context, id uuid.UUID, version int64, result Result) (int64, error) {
	if !json.Valid(result.Response) || len(result.Response) > 1<<20 || len(result.Reference) > 16384 || len(result.Matches) > 500 {
		return 0, ErrInvalid
	}
	bytes := len(result.Response) + len(result.Reference)
	seen := map[uuid.UUID]bool{}
	for _, m := range result.Matches {
		bytes += len(m.Payload) + len(m.EntityID) + len(m.Provider) + len(m.Name)
		if m.ID == uuid.Nil || seen[m.ID] || m.EntityID == "" || len(m.EntityID) > 512 || m.Provider == "" || len(m.Provider) > 128 || len(m.Name) > 1000 || !json.Valid(m.Payload) || len(m.Payload) > 1<<20 || math.IsNaN(m.Score) || math.IsInf(m.Score, 0) || (m.Status != "pending" && m.Status != "skipped") {
			return 0, ErrInvalid
		}
		seen[m.ID] = true
	}
	if bytes > 2<<20 {
		return 0, ErrInvalid
	}
	return s.mutate(ctx, id, version, func(tx *sql.Tx, status string, _ bool) error {
		if status != "processing" {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+s.schema+".screening_matches WHERE tenant_id=? AND screening_id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(id)); err != nil {
			return err
		}
		// One prepared statement, bounded at 500; no relationship lookup per row.
		stmt, err := tx.PrepareContext(ctx, "INSERT INTO "+s.schema+".screening_matches(tenant_id,screening_id,id,entity_id,provider,name,status,score,payload,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))")
		if err != nil {
			return err
		}
		defer stmt.Close()
		pending := false
		for _, m := range result.Matches {
			if m.Status == "pending" {
				pending = true
			}
			if _, err := stmt.ExecContext(ctx, mysqlstore.UUID(s.tenant), mysqlstore.UUID(id), mysqlstore.UUID(m.ID), m.EntityID, m.Provider, m.Name, m.Status, m.Score, mysqlstore.JSON(m.Payload)); err != nil {
				return err
			}
		}
		next := "no_hit"
		if pending || result.Partial {
			next = "awaiting_review"
		}
		return s.advance(ctx, tx, id, "status=?,response_json=?,provider_reference=?,partial=?,match_count=?,completed_at=IF(?='no_hit',UTC_TIMESTAMP(6),NULL),last_error='',failed_at=NULL", next, mysqlstore.JSON(result.Response), result.Reference, result.Partial, len(result.Matches), next)
	})
}
func (s *Store) Review(ctx context.Context, id, match uuid.UUID, version int64, next string) (int64, error) {
	if match == uuid.Nil || (next != "confirmed_hit" && next != "no_hit") {
		return 0, ErrInvalid
	}
	return s.mutate(ctx, id, version, func(tx *sql.Tx, status string, partial bool) error {
		if status != "awaiting_review" && status != "confirmed_hit" && status != "no_hit" {
			return ErrConflict
		}
		var current string
		if err := tx.QueryRowContext(ctx, "SELECT status FROM "+s.schema+".screening_matches WHERE tenant_id=? AND screening_id=? AND id=? FOR UPDATE", mysqlstore.UUID(s.tenant), mysqlstore.UUID(id), mysqlstore.UUID(match)).Scan(&current); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".screening_matches SET status=?,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND screening_id=? AND id=?", next, mysqlstore.UUID(s.tenant), mysqlstore.UUID(id), mysqlstore.UUID(match)); err != nil {
			return err
		}
		var pending, confirmed int
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(status='pending'),0),COALESCE(SUM(status='confirmed_hit'),0) FROM "+s.schema+".screening_matches WHERE tenant_id=? AND screening_id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(id)).Scan(&pending, &confirmed); err != nil {
			return err
		}
		state := "no_hit"
		if confirmed > 0 {
			state = "confirmed_hit"
		} else if pending > 0 || partial {
			state = "awaiting_review"
		}
		return s.advance(ctx, tx, id, "status=?,completed_at=IF(?='awaiting_review',NULL,UTC_TIMESTAMP(6))", state, state)
	})
}

// RetryDelay is bounded and independent of provider/SQL error strings.
func RetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return time.Second * time.Duration(1<<uint(attempt-1))
}
