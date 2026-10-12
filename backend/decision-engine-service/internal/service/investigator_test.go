package service

import "testing"

func TestDecisionReviewEligibility(t *testing.T) {
	for _, outcome := range []string{"approve", "decline", "block", "review", "block_and_review", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			pending := DecisionReview(outcome, "", nil)
			want := outcome == "review" || outcome == "block_and_review"
			if pending.Eligible != want || pending.MachineOutcome != outcome || pending.Version != "0" {
				t.Fatal(pending)
			}
			for _, status := range []string{"approve", "decline"} {
				reviewed := DecisionReview(outcome, status, nil)
				if reviewed.Eligible || reviewed.Status != status || reviewed.Version != "1" || reviewed.MachineOutcome != outcome {
					t.Fatal(reviewed)
				}
			}
		})
	}
}
