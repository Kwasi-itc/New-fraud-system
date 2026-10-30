package mysql

import (
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func effectQueuePolicies() map[ports.JobKind]EffectQueuePolicy {
	return map[ports.JobKind]EffectQueuePolicy{
		ports.JobWorkflow:  {Name: "workflow_executions", MaxAttempts: 3},
		ports.JobScreening: {Name: "screening_executions", MaxAttempts: 3},
		ports.JobScoring:   {Name: "scoring_requests", MaxAttempts: 3},
		ports.JobOutbox:    {Name: "outbox_events", MaxAttempts: 3},
	}
}

func TestEffectQueueConfigurationRequiresCompleteOwnedProducers(t *testing.T) {
	for _, change := range []func(map[ports.JobKind]EffectQueuePolicy){
		func(p map[ports.JobKind]EffectQueuePolicy) { delete(p, ports.JobOutbox) },
		func(p map[ports.JobKind]EffectQueuePolicy) { p[ports.JobAsync] = p[ports.JobOutbox] },
		func(p map[ports.JobKind]EffectQueuePolicy) {
			p[ports.JobOutbox] = EffectQueuePolicy{"invalid queue", 1}
		},
		func(p map[ports.JobKind]EffectQueuePolicy) { p[ports.JobOutbox] = EffectQueuePolicy{"outbox", 0} },
	} {
		policies := effectQueuePolicies()
		change(policies)
		if _, err := copyEffectQueues(policies); err == nil {
			t.Fatal("incomplete or invalid producer accepted")
		}
	}
	policies := effectQueuePolicies()
	copy, err := copyEffectQueues(policies)
	if err != nil {
		t.Fatal(err)
	}
	delete(policies, ports.JobOutbox)
	if len(copy) != 4 {
		t.Fatal("caller mutated store configuration")
	}
}
