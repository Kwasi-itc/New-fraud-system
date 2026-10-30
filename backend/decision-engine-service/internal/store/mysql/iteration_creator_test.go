package mysql

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDraftValidation(t *testing.T) {
	tenant, op := uuid.New(), uuid.New()
	good := DraftInput{ID: uuid.New(), ScenarioID: uuid.New(), TriggerFormula: json.RawMessage(`{"constant":true}`)}
	if _, err := validateDraft(tenant, op, good); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*DraftInput){
		func(i *DraftInput) { i.ID = uuid.Nil }, func(i *DraftInput) { i.ScenarioID = uuid.Nil },
		func(i *DraftInput) { i.TriggerFormula = json.RawMessage(`{`) }, func(i *DraftInput) { i.Schedule = "\x00" },
		func(i *DraftInput) { i.Schedule = strings.Repeat("x", (1<<20)+1) },
		func(i *DraftInput) { v := int(math.MaxInt32) + 1; i.ScoreReviewThreshold = &v },
		func(i *DraftInput) {
			a, b, c := 3, 2, 1
			i.ScoreReviewThreshold = &a
			i.ScoreBlockAndReviewThreshold = &b
			i.ScoreDeclineThreshold = &c
		},
	} {
		bad := good
		change(&bad)
		if _, err := validateDraft(tenant, op, bad); err == nil {
			t.Fatal("invalid draft accepted")
		}
	}
	if _, err := validateDraft(uuid.Nil, op, good); err == nil {
		t.Fatal("nil tenant")
	}
	if _, err := validateDraft(tenant, uuid.Nil, good); err == nil {
		t.Fatal("nil operation")
	}
}
