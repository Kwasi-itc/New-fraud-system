package installation

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"strings"
)

// NewExactKeyReadinessWithLookups binds the complete v006 shared lineage.
// Existing constructors remain strict about their supported version. This
// enables physical publication after ingestion's lookup upgrades without
// ignoring appended migration history or dropping dependency grant checks.
func NewExactKeyReadinessWithLookups(db *sql.DB, plan migrations.Plan, resolver, creation, result, recordLookup, contractLookup Definer) (*ExactKeyReadiness, error) {
	defs := []Definer{resolver, creation, result, recordLookup, contractLookup}
	for i, def := range defs {
		if _, err := def.SQLAccount(); err != nil {
			return nil, err
		}
		for _, previous := range defs[:i] {
			if def.User == previous.User && strings.EqualFold(def.Host, previous.Host) {
				return nil, errors.New("distinct lookup and owner dependency definers required")
			}
		}
	}
	base, err := Statements(plan.Database, resolver)
	if err != nil {
		return nil, err
	}
	ownerCount := len(base) + 9
	if len(plan.Steps) != ownerCount+2 && len(plan.Steps) != ownerCount+4 {
		return nil, errors.New("complete v006 shared lineage required")
	}
	previous := plan
	previous.Steps = append([]migrations.Step(nil), plan.Steps[:ownerCount]...)
	r, err := NewExactKeyReadinessWithOwnerResults(db, previous, resolver, creation, result)
	if err != nil {
		return nil, err
	}
	bound, err := ExtendRecordLookupPlan(previous, recordLookup, plan.Steps[ownerCount:ownerCount+1])
	if err != nil {
		return nil, err
	}
	bound, err = ExtendContractLookupPlan(bound, contractLookup, plan.Steps[ownerCount+1:ownerCount+2])
	if err != nil {
		return nil, err
	}
	if len(plan.Steps) == ownerCount+4 {
		bound, err = ExtendWorkerIndexPlan(bound, plan.Steps[ownerCount+2:])
		if err != nil {
			return nil, err
		}
	}
	r.runner, err = migrations.New(db, bound)
	if err != nil {
		return nil, err
	}
	r.recordLookupDefiner, r.contractLookupDefiner = &recordLookup, &contractLookup
	return r, nil
}

func (r *ExactKeyReadiness) verifyLookupDependencies(ctx context.Context, conn *sql.Conn, record Definer) error {
	for _, dependency := range []struct {
		def    *Definer
		policy func(Definer) AccountPolicy
	}{{r.recordLookupDefiner, RecordLookupDefinerPolicy}, {r.contractLookupDefiner, ContractLookupDefinerPolicy}} {
		if dependency.def == nil {
			continue
		}
		def := *dependency.def
		if record.User == def.User && strings.EqualFold(record.Host, def.Host) {
			return errors.New("record trigger definer must differ from lookup definers")
		}
		if err := VerifyAccountPolicy(ctx, conn, r.database, dependency.policy(def)); err != nil {
			return err
		}
		if err := VerifyNoAccountSessions(ctx, conn, def); err != nil {
			return err
		}
	}
	return nil
}
