package ast_eval

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type riskContractStub struct {
	value *platform.RiskSnapshot
	err   error
}

func (s riskContractStub) Create(context.Context, platform.RiskSnapshot) (platform.RiskSnapshot, error) {
	return platform.RiskSnapshot{}, errors.New("unexpected mutation in evaluator test")
}
func (s riskContractStub) GetByObject(context.Context, string, string, string) (*platform.RiskSnapshot, error) {
	return s.value, s.err
}

func TestRiskAbsenceIsPortableAndFailuresAreNotNoMatch(t *testing.T) {
	failure := errors.New("storage unavailable")
	for _, tc := range []struct {
		name string
		repo riskContractStub
		want any
		err  error
	}{
		{"present", riskContractStub{value: &platform.RiskSnapshot{RiskLevel: "high"}}, "high", nil},
		{"absent", riskContractStub{err: ports.ErrRiskSnapshotNotFound}, nil, nil},
		{"wrapped absence", riskContractStub{err: fmt.Errorf("lookup: %w", ports.ErrRiskSnapshotNotFound)}, nil, nil},
		{"failed", riskContractStub{err: failure}, nil, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, err := EvaluateNode(context.Background(), ast.Node{Function: "record_risk_level"}, Runtime{RiskRepo: tc.repo})
			if value != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("value=%v error=%v, want=%v error=%v", value, err, tc.want, tc.err)
			}
		})
	}
}

func TestRiskNilSuccessIsNotConfirmedAbsence(t *testing.T) {
	if _, err := EvaluateNode(context.Background(), ast.Node{Function: "record_risk_level"}, Runtime{RiskRepo: riskContractStub{}}); err == nil {
		t.Fatal("unimplemented/malformed repository response became no risk")
	}
}
