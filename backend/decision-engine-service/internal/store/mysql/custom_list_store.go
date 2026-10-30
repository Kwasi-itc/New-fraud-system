package mysql

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// CustomListStore keeps the tenant's list and entry mutations atomic. Names use
// MySQL LOWER(utf8mb4_0900_as_cs) followed by exact binary comparison: case-folded,
// accent/whitespace-sensitive, not PostgreSQL locale or all UCA expansions.
// Values remain byte-exact. Linked entries derive names from their parent;
// nullable list_id retains the source's standalone legacy-entry representation.
type CustomListStore struct{ configuration *AuthoringStore }

func NewCustomListStore(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, authorize AuthoringAuthorizer) (*CustomListStore, error) {
	c, err := NewAuthoringStore(db, database, tenant, maxRows, maxBytes, authorize)
	if err != nil {
		return nil, err
	}
	return &CustomListStore{c}, nil
}
func (s *CustomListStore) Mutate(ctx context.Context, fn func(CustomListRepository) error) error {
	return s.run(ctx, true, fn)
}
func (s *CustomListStore) ReadSnapshot(ctx context.Context, fn func(CustomListRepository) error) error {
	return s.run(ctx, false, fn)
}
func (s *CustomListStore) run(ctx context.Context, write bool, fn func(CustomListRepository) error) error {
	if s == nil || s.configuration == nil || fn == nil {
		return errors.New("custom list store and callback required")
	}
	c := s.configuration.configuration
	run := mysqlstore.Snapshot
	if write {
		run = mysqlstore.Mutation
	}
	return run(ctx, c.db, func(tx *sql.Tx) error {
		action := AuthoringCustomListRead
		if write {
			action = AuthoringCustomListWrite
		}
		if err := s.configuration.authorize(ctx, tx, c.tenant, action); err != nil {
			return err
		}
		return fn(CustomListRepository{authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, write}}})
	})
}
