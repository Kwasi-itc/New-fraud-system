package casepkg

import "testing"

func TestValidateStatusTransition(t *testing.T) {
	tests := []struct {
		name    string
		current Status
		next    Status
		wantErr bool
	}{
		{name: "pending to investigating", current: StatusPending, next: StatusInvestigating},
		{name: "pending to closed", current: StatusPending, next: StatusClosed},
		{name: "investigating to closed", current: StatusInvestigating, next: StatusClosed},
		{name: "closed to investigating", current: StatusClosed, next: StatusInvestigating},
		{name: "investigating to pending rejected", current: StatusInvestigating, next: StatusPending, wantErr: true},
		{name: "closed to pending rejected", current: StatusClosed, next: StatusPending, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStatusTransition(tt.current, tt.next)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidReviewLevel(t *testing.T) {
	for _, level := range []string{"probable_false_positive", "investigate", "escalate"} {
		if !ValidReviewLevel(level) {
			t.Fatalf("expected %s to be valid", level)
		}
	}
	if ValidReviewLevel("manual_review") {
		t.Fatal("expected manual_review to be invalid")
	}
}
