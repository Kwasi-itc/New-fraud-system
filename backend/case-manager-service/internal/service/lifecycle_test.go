package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/google/uuid"
)

func TestClosingFindingsValidation(t *testing.T) {
	for _, tt := range []struct {
		name, comment string
		outcome       casepkg.Outcome
		valid         bool
	}{
		{"valid", " findings ", casepkg.OutcomeConfirmedRisk, true},
		{"blank", " \t\n", casepkg.OutcomeConfirmedRisk, false},
		{"unicode whitespace", "\u2003", casepkg.OutcomeFalsePositive, false},
		{"unset", "findings", casepkg.OutcomeUnset, false},
		{"unknown", "findings", "unknown", false},
		{"byte boundary", strings.Repeat("é", 5000), casepkg.OutcomeValuableAlert, true},
		{"too many bytes", strings.Repeat("é", 5001), casepkg.OutcomeFalsePositive, false},
		{"padding counts", strings.Repeat(" ", 10000) + "x", casepkg.OutcomeFalsePositive, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validateClosingFindings(tt.outcome, tt.comment)
			if (err == nil) != tt.valid || (err != nil && !errors.Is(err, casepkg.ErrValidation)) {
				t.Fatalf("valid=%v err=%v", tt.valid, err)
			}
		})
	}
}

func TestLifecycleRejectsUntrustedActorsAndPatchBypass(t *testing.T) {
	tenant := uuid.New()
	s := NewCaseService(nil, nil, ports.Repositories{}, &testUOW{})
	for _, ctx := range []context.Context{context.Background(), access.WithPrincipal(context.Background(), access.Principal{Kind: access.Service, Subject: "integration", ServiceTenants: []uuid.UUID{tenant}})} {
		if _, err := s.CloseCase(ctx, tenant, uuid.New(), casepkg.OutcomeConfirmedRisk, "findings"); !errors.Is(err, casepkg.ErrForbidden) {
			t.Fatalf("untrusted close: %v", err)
		}
	}
	ctx := access.WithPrincipal(context.Background(), access.Principal{Kind: access.User, Subject: "analyst", TenantID: tenant})
	closed, investigating := casepkg.StatusClosed, casepkg.StatusInvestigating
	unset, risk := casepkg.OutcomeUnset, casepkg.OutcomeConfirmedRisk
	for _, input := range []UpdateCaseInput{{Status: &closed}, {Status: &closed, Outcome: &risk}, {Outcome: &unset}, {Outcome: &risk}, {Status: &investigating, Outcome: &risk}} {
		input.TenantID = tenant
		if _, err := s.UpdateCase(ctx, input, nil); !errors.Is(err, casepkg.ErrValidation) {
			t.Fatalf("PATCH bypass: %+v %v", input, err)
		}
	}
}
