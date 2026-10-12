package service

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

type WorkflowAuthoringStore interface {
	List(context.Context, string, string, workflow.AuthoringActor) (workflow.AuthoringPage, error)
	Save(context.Context, string, string, workflow.AuthoringActor, workflow.CaseRule) (workflow.CaseRule, error)
}
type WorkflowAuthoringService struct {
	Store WorkflowAuthoringStore
	Cache DecisionMetadataCacheInvalidator
}

func (s WorkflowAuthoringService) List(ctx context.Context, t, sc string, a workflow.AuthoringActor) (workflow.AuthoringPage, error) {
	return s.Store.List(ctx, t, sc, a)
}
func (s WorkflowAuthoringService) Save(ctx context.Context, t, sc string, a workflow.AuthoringActor, r workflow.CaseRule) (workflow.CaseRule, error) {
	if err := r.Validate(); err != nil {
		return workflow.CaseRule{}, err
	}
	result, err := s.Store.Save(ctx, t, sc, a, r)
	if err == nil && s.Cache != nil {
		s.Cache.InvalidateWorkflowRules(ctx, t, sc)
	}
	return result, err
}
