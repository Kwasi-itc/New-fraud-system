package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/google/uuid"
)

// SavePublished is an internal owner coordinator, not an enabled HTTP endpoint.
// authorize must authenticate/bind actor and authorize tenant/table on EVERY call,
// including historical replay. It runs before any database locks or transaction.
// effects must perform short local owner revision/index/queue work on tx, with no
// DDL, network, commit or table lock-order inversion. It must leave the ingestion
// model semantically unchanged: this method coordinates display-only edits, not
// physical changes. The caller must supply trusted actor identity, never a header.
// New saves require an open registry gate matching the existing contract; the
// shared gate is held through commit. A changed ingestion token rolls back all
// effects. Historical receipts do not attest current physical readiness.
//
// The result is the original ingestion publication receipt, NOT current or saved
// display settings. A separate authorized read obtains current settings. This is
// not a drop-in replacement for the existing settings-returning API.
func (s *DisplayStore) SavePublished(ctx context.Context, table, actor, operationID uuid.UUID, condition PublicationCondition, in display.SaveRequest, publication *PublicationStore,
	authorize func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error, effects func(context.Context, *sql.Tx) error,
) (contract.Publication, error) {
	state, err := s.savePublished(ctx, table, actor, operationID, condition, in, publication, authorize, effects, false)
	if err != nil {
		return contract.Publication{}, err
	}
	return state.result, nil
}

// SavePublishedSettings returns the immutable settings saved by this operation,
// including historical replay. Requires the append-only v004 owner-result schema.
// Missing legacy results fail closed; no current-state reconstruction is allowed.
func (s *DisplayStore) SavePublishedSettings(ctx context.Context, table, actor, operationID uuid.UUID, condition PublicationCondition, in display.SaveRequest, publication *PublicationStore,
	authorize func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error, effects func(context.Context, *sql.Tx) error,
) (display.Settings, error) {
	state, err := s.savePublished(ctx, table, actor, operationID, condition, in, publication, authorize, effects, true)
	if err != nil {
		return display.Settings{}, err
	}
	return state.settings, nil
}
func (s *DisplayStore) savePublished(ctx context.Context, table, actor, operationID uuid.UUID, condition PublicationCondition, in display.SaveRequest, publication *PublicationStore,
	authorize func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error, effects func(context.Context, *sql.Tx) error, retainResult bool,
) (*displayPublication, error) {
	if publication == nil || publication.schema != s.schema || publication.db != s.db || authorize == nil || effects == nil || operationID == uuid.Nil {
		return nil, errors.New("same-pool publication, operation, authorization and owner effects required")
	}
	if len(in.Order)+len(in.Displayed)+len(in.Display)+len(in.DisplayColumns) > s.maxRows {
		return nil, ErrSnapshotLimit
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	if len(raw) > s.maxBytes {
		return nil, ErrSnapshotLimit
	}
	hash, err := display.SaveRequestHash(s.tenant, table, actor, condition, in)
	if err != nil {
		return nil, err
	}
	if err := authorize(ctx, s.tenant, table, actor); err != nil {
		return nil, err
	}
	operation := PublicationOperation{ID: operationID, RequestHash: hash, Condition: condition}
	// Historical replay must not depend on today's relation topology or stale
	// optimistic display version. This transaction ends before broader locks.
	prior, err := publication.RecoverPublication(ctx, s.tenant, table, operation)
	if err == nil {
		state := &displayPublication{result: prior}
		if retainResult {
			state.settings, err = s.readDisplayResult(ctx, s.db, table, operationID)
			if err != nil {
				return nil, err
			}
		}
		return state, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	state := &displayPublication{store: publication, operation: operation, retainResult: retainResult}
	if _, err = s.save(ctx, table, in, effects, state); err != nil {
		return nil, err
	}
	return state, nil
}
