package mysql

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/google/uuid"
)

// BrowserWorkflowAdmin binds the actor to the verified session supplied by the
// runtime. Neither a request body nor gateway headers can override that actor.
type BrowserWorkflowAdmin struct {
	owner      *BrowserWorkflowStore
	copyReader *TenantDataReader
}

func NewBrowserWorkflowAdmin(owner *BrowserWorkflowStore) (*BrowserWorkflowAdmin, error) {
	if owner == nil || owner.configuration == nil || owner.authorize == nil {
		return nil, ErrOwnerContractUnavailable
	}
	return &BrowserWorkflowAdmin{owner: owner}, nil
}
func (s *BrowserWorkflowAdmin) MatchesExecutionStore(e *ExecutionStore) bool {
	return s != nil && s.owner != nil && (&DecisionStore{s.owner.configuration}).MatchesExecutionStore(e)
}
func (s *BrowserWorkflowAdmin) binding() (string, workflow.AuthoringActor, error) {
	if s == nil || s.owner == nil || s.owner.configuration == nil {
		return "", workflow.AuthoringActor{}, ErrOwnerContractUnavailable
	}
	v := s.owner.session
	return s.owner.configuration.tenant.String(), workflow.AuthoringActor{UserID: v.ActorID.String(), SessionID: v.SessionID.String(), Version: v.MembershipVersion}, nil
}
func (s *BrowserWorkflowAdmin) List(ctx context.Context, parent uuid.UUID) (workflow.AuthoringPage, error) {
	tenant, actor, e := s.binding()
	if e != nil {
		return workflow.AuthoringPage{}, e
	}
	return s.owner.List(ctx, tenant, parent.String(), actor)
}
func (s *BrowserWorkflowAdmin) Save(ctx context.Context, parent uuid.UUID, in workflow.CaseRule) (workflow.CaseRule, error) {
	tenant, actor, e := s.binding()
	if e != nil {
		return workflow.CaseRule{}, e
	}
	return s.owner.Save(ctx, tenant, parent.String(), actor, in)
}
func (s *BrowserWorkflowAdmin) Delete(ctx context.Context, parent, id uuid.UUID, expected time.Time) error {
	tenant, actor, e := s.binding()
	if e != nil {
		return e
	}
	return s.owner.Delete(ctx, tenant, parent.String(), actor, id.String(), expected)
}
