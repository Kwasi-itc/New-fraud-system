package service

import identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"

// DecisionReview keeps human adjudication separate from the machine outcome.
// The shared command runner validates eligibility, action and expected version
// before calling this with a non-nil command and stores the immutable receipt.
func DecisionReview(outcome, status string, command *identity.Command) identity.State {
	state := identity.State{Status: "pending", Version: "0", MachineOutcome: outcome, Eligible: outcome == "review" || outcome == "block_and_review", Actions: []string{"approve", "decline"}}
	if status != "" {
		state.Status = status
		state.Version = "1"
		state.Eligible = false
	}
	if !state.Eligible && state.Version == "0" {
		state.Status = "not_applicable"
	}
	if command != nil {
		state.Status = command.Action
		state.Version = "1"
		state.Eligible = false
	}
	return state
}
