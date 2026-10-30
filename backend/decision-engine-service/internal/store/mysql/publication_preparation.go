package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"strings"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// PublicationPreparation reads an immutable committed version with publish
// authority before model/provider work. No locks or transaction escape this call.
func (s *AuthoringStore) PublicationPreparation(ctx context.Context, parent, iteration uuid.UUID) (out PublicationSnapshot, err error) {
	if s == nil || s.configuration == nil || parent == uuid.Nil || iteration == uuid.Nil {
		return out, ErrTenantScope
	}
	c := s.configuration
	err = mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, c.tenant, AuthoringPublish); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		var err error
		out.Scenario, err = (ScenarioRepository{r}).GetByID(ctx, c.tenant.String(), parent.String())
		if err != nil {
			return err
		}
		out.Iteration, err = (ScenarioIterationRepository{r}).GetByID(ctx, c.tenant.String(), parent.String(), iteration.String())
		if err != nil {
			return err
		}
		if out.Iteration.Status != scenario.IterationStatusCommitted {
			return ErrCommittedIteration
		}
		out.Rules, err = (RuleRepository{r}).ListByIteration(ctx, c.tenant.String(), parent.String(), iteration.String())
		return err
	})
	if err != nil {
		return PublicationSnapshot{}, err
	}
	return out, nil
}

// PreparedPublicationBinding cannot be supplied by JSON. Full portable AST/type
// validation must precede construction by the target publication coordinator.
// The private digest rejects any parent/selected-version/rule change between
// preparation and commit. Registry gates are rebound on the commit transaction.
type PreparedPublicationBinding struct {
	owner     *AuthoringStore
	snapshot  PublicationSnapshot
	digest    [32]byte
	guard     func(context.Context, *sql.Tx) error
	authorize func(context.Context, *sql.Tx) error
}

func (r TenantDataReader) MatchesPublicationScope(s *AuthoringStore) bool {
	if r.configuration == nil || s == nil || s.configuration == nil {
		return false
	}
	a, b := r.configuration, s.configuration
	return a.db == b.db && a.schema == b.schema && a.tenant == b.tenant && a.maxRows == b.maxRows && a.maxBytes == b.maxBytes
}

func (r TenantDataReader) PreparePublicationBinding(ctx context.Context, owner *AuthoringStore, snapshot PublicationSnapshot, model ports.TenantModel) (*PreparedPublicationBinding, error) {
	if !r.MatchesPublicationScope(owner) || snapshot.Scenario.TenantID != r.configuration.tenant.String() || snapshot.Iteration.TenantID != snapshot.Scenario.TenantID || snapshot.Iteration.ScenarioID != snapshot.Scenario.ID {
		return nil, ErrTenantScope
	}
	c := r.configuration
	// Detach provider maps before building the commit guard. Neither a caller
	// nor a provider refresh may mutate prepared registry authority afterwards.
	modelJSON, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	if len(modelJSON) > c.maxBytes {
		return nil, ErrProjectionLimit
	}
	var detached ports.TenantModel
	if err := json.Unmarshal(modelJSON, &detached); err != nil {
		return nil, err
	}
	model = detached
	queries, err := aggregatePublicationQueries(ctx, snapshot, model, c.maxRows, c.maxBytes)
	if err != nil {
		return nil, err
	}
	names, err := publicationRelatedTables(snapshot, model, c.maxBytes)
	if err != nil {
		return nil, err
	}
	guard, err := r.executionRegistryGuard(names, queries, model)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	if len(raw) > c.maxBytes {
		return nil, ErrProjectionLimit
	}
	return &PreparedPublicationBinding{owner, snapshot, sha256.Sum256(raw), guard, func(ctx context.Context, tx *sql.Tx) error { return c.authorize(ctx, tx, c.tenant, false) }}, nil
}

// Include every intermediate related_field table, not only the final target.
// The complete AST validator rejects dynamic/invalid paths before this step;
// this traversal independently fails closed and retains bounded work.
func publicationRelatedTables(snapshot PublicationSnapshot, model ports.TenantModel, maxBytes int) (map[string]bool, error) {
	names := map[string]bool{snapshot.Scenario.TriggerObjectType: true}
	nodes, used := 0, 0
	var visit func(domainast.Node, int) error
	visit = func(node domainast.Node, depth int) error {
		nodes++
		if nodes > 10000 || depth > 64 {
			return ErrProjectionLimit
		}
		if strings.EqualFold(strings.TrimSpace(node.Function), "related_field") {
			path, ok := node.NamedChildren["path"].Constant.(string)
			if !ok || path == "" {
				return ErrPublishedModelUnavailable
			}
			current, ok := model.Tables[snapshot.Scenario.TriggerObjectType]
			if !ok || current.Archived {
				return ErrPublishedModelUnavailable
			}
			for _, segment := range strings.Split(path, ".") {
				link, ok := current.LinksToSingle[strings.TrimSpace(segment)]
				if !ok {
					return ErrPublishedModelUnavailable
				}
				current, ok = model.Tables[link.ParentTableName]
				if !ok || current.Archived {
					return ErrPublishedModelUnavailable
				}
				names[link.ParentTableName] = true
			}
		}
		for _, child := range node.Children {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		for _, child := range node.NamedChildren {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	formulas := []json.RawMessage{snapshot.Iteration.TriggerFormula}
	for _, rule := range snapshot.Rules {
		formulas = append(formulas, rule.Formula)
	}
	for _, raw := range formulas {
		used += len(raw)
		if used > maxBytes {
			return nil, ErrProjectionLimit
		}
		var node domainast.Node
		if err := json.Unmarshal(raw, &node); err != nil {
			return nil, err
		}
		if err := visit(node, 0); err != nil {
			return nil, err
		}
	}
	return names, nil
}

func (s *AuthoringStore) PublishPrepared(ctx context.Context, input PublicationInput, prepared *PreparedPublicationBinding) ([]scenario.Publication, error) {
	if prepared == nil || prepared.owner != s || prepared.guard == nil || prepared.authorize == nil || prepared.snapshot.Scenario.ID != input.ScenarioID.String() || prepared.snapshot.Iteration.ID != input.IterationID.String() {
		return nil, ErrTenantScope
	}
	return s.publication(ctx, input, false, func(ctx context.Context, tx *sql.Tx, current PublicationSnapshot) error {
		raw, err := json.Marshal(current)
		if err != nil {
			return err
		}
		if sha256.Sum256(raw) != prepared.digest {
			return ErrExecutionBindingChanged
		}
		return prepared.guard(ctx, tx)
	}, prepared.authorize)
}
