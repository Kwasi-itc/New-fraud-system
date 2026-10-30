package service

import (
	"context"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

// WithDecisionTransactions configures only the narrow evaluation persistence
// path, without pretending a target implements legacy unfenced worker methods.
// It returns a copy for composition before serving requests. No runtime selector
// is changed; unavailable persistence is an explicit construction error.
func (s DecisionService) WithDecisionTransactions(manager ports.DecisionTransactionManager) (DecisionService, error) {
	if manager == nil {
		return DecisionService{}, errors.New("decision transaction manager required")
	}
	s.decisionTransactions = manager
	return s, nil
}

func (s DecisionService) runDecisionMutation(ctx context.Context, fn func(ports.DecisionMutationStore) error) error {
	if fn == nil {
		return errors.New("decision mutation callback required")
	}
	if s.decisionTransactions != nil {
		return s.decisionTransactions.RunDecisionMutation(ctx, fn)
	}
	if s.txManager == nil {
		return errors.New("decision persistence is not configured")
	}
	// Source compatibility: its existing complete transaction manager already
	// supplies this smaller capability. There is no fallback after a target
	// manager error and no second transaction or successful missing producer.
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if store == nil {
			return errors.New("decision mutation store unavailable")
		}
		return fn(store)
	})
}
