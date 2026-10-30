package mysql

import (
	"context"
	"database/sql"
	"errors"
)

type effectListAccess struct {
	authorize AuthoringAuthorizer
	write     bool
}

// WithCustomLists explicitly grants this staged transaction access to custom
// lists through a distinct authorizer. Evaluation normally requests reads only;
// generic decision persistence permission never confers list mutation rights.
// The copy does not change the original store or activate runtime composition.
func (s *EffectStore) WithCustomLists(authorize AuthoringAuthorizer, allowMutations bool) (*EffectStore, error) {
	if s == nil || s.configuration == nil || authorize == nil {
		return nil, errors.New("effect store and distinct custom-list authorizer required")
	}
	copy := *s
	copy.lists = &effectListAccess{authorize: authorize, write: allowMutations}
	return &copy, nil
}

// CustomLists returns an explicitly configured capability or an error, never
// a success-shaped no-op. It uses the exact decision/effect/job transaction.
func (r EffectRepositories) CustomLists() (CustomListRepository, error) {
	if r.customLists == nil {
		return CustomListRepository{}, ErrDecisionAuthorizationUnavailable
	}
	return *r.customLists, nil
}

func (s *EffectStore) authorizedCustomLists(ctx context.Context, tx *sql.Tx, write bool) (*CustomListRepository, error) {
	if s == nil || s.configuration == nil || tx == nil {
		return nil, ErrDecisionAuthorizationUnavailable
	}
	if s.lists == nil {
		return nil, nil // Optional capability is unavailable, not silently authorized.
	}
	c := s.configuration
	mutate := write && s.lists.write
	action := AuthoringCustomListRead
	if mutate {
		action = AuthoringCustomListWrite
	}
	// Recheck before any owner lock, avoiding authorization-lock inversion when
	// a later workflow action touches a list. Never lock across network work.
	if err := s.lists.authorize(ctx, tx, c.tenant, action); err != nil {
		return nil, err
	}
	r := CustomListRepository{authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, mutate}}}
	return &r, nil
}
