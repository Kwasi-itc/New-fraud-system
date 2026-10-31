package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// BootstrapPass is trusted deployment code for one owner/pass in ONE logical
// database. Split structural installation, fixed routines/grants and compatibility
// into separate passes when there are cross-owner dependencies. Verify must check
// the complete reviewed lineage AND privileges required by that pass. Callbacks
// must not make network calls or retain connections. No callback is optional.
type BootstrapPass struct {
	Name      string
	Database  string
	DependsOn []string
	Apply     func(context.Context) error
	Verify    func(context.Context) error
}

// Bootstrap validates the entire graph before any write, serializes bootstrap
// coordinators for this database, and verifies each prerequisite before applying
// its consumer. Direct/independent installers must obey the same deployment lock
// protocol. No rollback/down or automatic retry is attempted after a failed pass.
// A pinned lock uses one connection; callbacks need additional bounded capacity.
func Bootstrap(ctx context.Context, db *sql.DB, database string, passes []BootstrapPass) error {
	if db == nil || db.Stats().MaxOpenConnections < 2 {
		return errors.New("bootstrap requires a bounded pool with at least two connections")
	}
	order, err := bootstrapOrder(database, passes)
	if err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(database))
	return mysqlstore.WithDDLLock(ctx, db, fmt.Sprintf("shared_bootstrap_%x", hash[:22]), func(*sql.Conn) error {
		for _, i := range order {
			if err := ctx.Err(); err != nil {
				return err
			}
			for _, name := range passes[i].DependsOn {
				for j := range passes {
					if passes[j].Name == name {
						if err := passes[j].Verify(ctx); err != nil {
							return fmt.Errorf("bootstrap prerequisite %s: %w", name, err)
						}
					}
				}
			}
			if err := passes[i].Apply(ctx); err != nil {
				return fmt.Errorf("bootstrap apply %s: %w", passes[i].Name, err)
			}
			if err := passes[i].Verify(ctx); err != nil {
				return fmt.Errorf("bootstrap verify %s: %w", passes[i].Name, err)
			}
		}
		return ctx.Err()
	})
}

func bootstrapOrder(database string, passes []BootstrapPass) ([]int, error) {
	if _, err := mysqlstore.Identifier(database); err != nil {
		return nil, err
	}
	if len(passes) < 1 || len(passes) > 256 {
		return nil, errors.New("bootstrap requires 1..256 explicit passes")
	}
	indices := make(map[string]int, len(passes))
	for i, p := range passes {
		_, err := mysqlstore.Identifier(p.Name)
		if _, exists := indices[p.Name]; err != nil || exists || p.Database != database || p.Apply == nil || p.Verify == nil {
			return nil, errors.New("unique passes in the same database with apply/verify required")
		}
		indices[p.Name] = i
	}
	state := make([]uint8, len(passes))
	order := make([]int, 0, len(passes))
	var visit func(int) error
	visit = func(i int) error {
		if state[i] == 1 {
			return errors.New("cyclic bootstrap dependencies")
		}
		if state[i] == 2 {
			return nil
		}
		state[i] = 1
		seen := map[string]bool{}
		for _, name := range passes[i].DependsOn {
			j, ok := indices[name]
			if !ok || seen[name] {
				return errors.New("missing or duplicate bootstrap dependency")
			}
			seen[name] = true
			if err := visit(j); err != nil {
				return err
			}
		}
		state[i] = 2
		order = append(order, i)
		return nil
	}
	for i := range passes {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	return order, nil
}
