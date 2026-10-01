package service

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"testing"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/google/uuid"
)

type testUOW struct {
	repos       ports.Repositories
	calls       int
	commitError error
}

func (u *testUOW) WithinTransaction(_ context.Context, fn func(ports.Repositories) error) error {
	u.calls++
	if err := fn(u.repos); err != nil {
		return err
	}
	return u.commitError
}

func TestNestedTransactionAndCommitFailure(t *testing.T) {
	commitErr := errors.New("commit failed")
	uow := &testUOW{commitError: commitErr}
	s := NewCaseService(nil, nil, ports.Repositories{}, uow)
	result, err := transact(context.Background(), s, func(tx CaseService) (string, error) {
		return transact(context.Background(), tx, func(CaseService) (string, error) { return "must not escape failed commit", nil })
	})
	if !errors.Is(err, commitErr) || result != "" || uow.calls != 1 {
		t.Fatalf("result=%q err=%v transactions=%d", result, err, uow.calls)
	}
}

func TestValidateUpdateBeforeRepositoryAccess(t *testing.T) {
	name, outcome, level := "   ", casepkg.Outcome("unknown"), "made-up"
	for _, input := range []UpdateCaseInput{
		{Name: &name}, {Outcome: &outcome}, {ReviewLevel: &level}, {ClearReviewLevel: true, ReviewLevel: &level},
	} {
		// Nil repositories deliberately fail if validation attempts database access.
		s := NewCaseService(nil, nil, ports.Repositories{}, &testUOW{})
		input.TenantID = uuid.New()
		ctx := access.WithPrincipal(context.Background(), access.Principal{Kind: access.User, Subject: "tester", TenantID: input.TenantID, Admin: true})
		if _, err := s.UpdateCase(ctx, input, nil); !errors.Is(err, casepkg.ErrValidation) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

type caseReader struct {
	ports.CaseRepository
	item casepkg.Case
}

func (r caseReader) LockShared(context.Context, uuid.UUID, uuid.UUID) (casepkg.Case, error) {
	return r.item, nil
}

type failingDecisionReader struct {
	ports.DecisionLinkRepository
	err error
}

func (r failingDecisionReader) ListByCase(context.Context, uuid.UUID, uuid.UUID) ([]casepkg.DecisionLink, error) {
	return nil, r.err
}

func TestDetailReadPropagatesRelationshipFailure(t *testing.T) {
	errRead := errors.New("database unavailable")
	item := casepkg.Case{ID: uuid.New(), TenantID: uuid.New()}
	repos := ports.Repositories{Cases: caseReader{item: item}, Inboxes: emptyInbox{}, Decisions: failingDecisionReader{err: errRead}, Contributors: emptyContributors{}}
	s := NewCaseService(nil, nil, repos, &testUOW{repos: repos})
	ctx := access.WithPrincipal(context.Background(), access.Principal{Kind: access.User, Subject: "tester", TenantID: item.TenantID, Admin: true})
	got, err := s.GetCase(ctx, item.TenantID, item.ID)
	if !errors.Is(err, errRead) || got.ID != uuid.Nil {
		t.Fatalf("partial detail returned: %+v %v", got, err)
	}
}

type emptyContributors struct{ ports.ContributorRepository }

type emptyInbox struct{ ports.InboxRepository }

func (emptyInbox) LockShared(context.Context, uuid.UUID, uuid.UUID) (casepkg.Inbox, error) {
	return casepkg.Inbox{}, nil
}

func (emptyContributors) List(context.Context, uuid.UUID, uuid.UUID) ([]casepkg.Contributor, error) {
	return nil, nil
}
