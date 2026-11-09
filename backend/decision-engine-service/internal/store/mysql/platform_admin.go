package mysql

import (
	"context"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

// PlatformAdmin uses separate read and explicit platform-mutation authority.
// Evaluation persistence permission is never a substitute for administration.
type PlatformAdmin struct {
	reads, writes *PlatformStore
	ids           ports.IDGenerator
	clock         ports.Clock
}

func NewPlatformAdmin(reads *PlatformStore, mutate PlatformAuthorizer, ids ports.IDGenerator, clock ports.Clock) (*PlatformAdmin, error) {
	if reads == nil || reads.db == nil || reads.authorize == nil || ids == nil || clock == nil {
		return nil, ErrOwnerContractUnavailable
	}
	var writes *PlatformStore
	if mutate != nil {
		copy := *reads
		copy.authorize = mutate
		writes = &copy
	}
	return &PlatformAdmin{reads, writes, ids, clock}, nil
}
func (s *PlatformAdmin) MatchesExecutionStore(e *ExecutionStore) bool {
	return s != nil && (&DecisionStore{s.reads}).MatchesExecutionStore(e)
}
func platformAdminRun[T any](ctx context.Context, s *PlatformAdmin, write bool, fn func(PlatformRepositories) (T, error)) (out T, err error) {
	if s == nil || s.reads == nil || write && s.writes == nil {
		return out, ErrOwnerContractUnavailable
	}
	run := s.reads.ReadSnapshot
	if write {
		run = s.writes.Mutate
	}
	err = run(ctx, func(r PlatformRepositories) error { out, err = fn(r); return err })
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}
func (s *PlatformAdmin) identity() (string, time.Time, error) {
	id := s.ids.New()
	if id == uuid.Nil {
		return "", time.Time{}, ErrOwnerContractUnavailable
	}
	return id.String(), s.clock.Now().UTC().Truncate(time.Microsecond), nil
}
func platformAdminText(r platformRepository, values ...string) error {
	if e := r.scope(r.tenant.String(), values...); e != nil {
		return errors.Join(ErrAdministrationInput, e)
	}
	return nil
}
func (s *PlatformAdmin) Tags(ctx context.Context, kind, object string) ([]platform.RecordTag, error) {
	return platformAdminRun(ctx, s, false, func(r PlatformRepositories) ([]platform.RecordTag, error) {
		if e := platformAdminText(r.RecordTags.platformRepository, kind, object); e != nil {
			return nil, e
		}
		return r.RecordTags.ListByObject(ctx, s.reads.tenant.String(), kind, object)
	})
}
func (s *PlatformAdmin) Flags(ctx context.Context, ip string) ([]platform.IPFlag, error) {
	return platformAdminRun(ctx, s, false, func(r PlatformRepositories) ([]platform.IPFlag, error) {
		if e := platformAdminText(r.IPFlags.platformRepository, ip); e != nil {
			return nil, e
		}
		return r.IPFlags.ListByIP(ctx, s.reads.tenant.String(), ip)
	})
}
func (s *PlatformAdmin) CreateTag(ctx context.Context, kind, object, tag string) (platform.RecordTag, error) {
	return platformAdminRun(ctx, s, true, func(r PlatformRepositories) (platform.RecordTag, error) {
		var v platform.RecordTag
		if e := platformAdminText(r.RecordTags.platformRepository, kind, object, tag); e != nil {
			return v, e
		}
		id, at, e := s.identity()
		if e != nil {
			return v, e
		}
		return r.RecordTags.Create(ctx, platform.RecordTag{ID: id, TenantID: s.reads.tenant.String(), ObjectType: kind, ObjectID: object, Tag: tag, CreatedAt: at})
	})
}
func (s *PlatformAdmin) CreateRisk(ctx context.Context, kind, object, level string) (platform.RiskSnapshot, error) {
	return platformAdminRun(ctx, s, true, func(r PlatformRepositories) (platform.RiskSnapshot, error) {
		var v platform.RiskSnapshot
		if e := platformAdminText(r.RiskSnapshots.platformRepository, kind, object, level); e != nil {
			return v, e
		}
		id, at, e := s.identity()
		if e != nil {
			return v, e
		}
		return r.RiskSnapshots.Create(ctx, platform.RiskSnapshot{ID: id, TenantID: s.reads.tenant.String(), ObjectType: kind, ObjectID: object, RiskLevel: level, CreatedAt: at})
	})
}
func (s *PlatformAdmin) CreateFlag(ctx context.Context, ip, flag string) (platform.IPFlag, error) {
	return platformAdminRun(ctx, s, true, func(r PlatformRepositories) (platform.IPFlag, error) {
		var v platform.IPFlag
		if e := platformAdminText(r.IPFlags.platformRepository, ip, flag); e != nil {
			return v, e
		}
		id, at, e := s.identity()
		if e != nil {
			return v, e
		}
		return r.IPFlags.Create(ctx, platform.IPFlag{ID: id, TenantID: s.reads.tenant.String(), IPAddress: ip, Flag: flag, CreatedAt: at})
	})
}
