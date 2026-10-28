package mysql

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDeliveryRedriveValidation(t *testing.T) {
	valid := DeliveryRedriveRequest{RequestID: uuid.New(), TenantID: uuid.New(), JobID: uuid.New(), SourceID: uuid.New(), Actor: "operator", Reason: "repair verified", ScheduledAt: time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)}
	cases := []func(*DeliveryRedriveRequest){
		func(r *DeliveryRedriveRequest) { r.RequestID = uuid.Nil },
		func(r *DeliveryRedriveRequest) { r.TenantID = uuid.Nil },
		func(r *DeliveryRedriveRequest) { r.JobID = uuid.Nil },
		func(r *DeliveryRedriveRequest) { r.SourceID = uuid.Nil },
		func(r *DeliveryRedriveRequest) { r.Actor = " " },
		func(r *DeliveryRedriveRequest) { r.Actor = strings.Repeat("a", 257) },
		func(r *DeliveryRedriveRequest) { r.Actor = string([]byte{255}) },
		func(r *DeliveryRedriveRequest) { r.Reason = "\n" },
		func(r *DeliveryRedriveRequest) { r.Reason = strings.Repeat("a", 2049) },
		func(r *DeliveryRedriveRequest) { r.Reason = string([]byte{255}) },
		func(r *DeliveryRedriveRequest) { r.ScheduledAt = time.Time{} },
		func(r *DeliveryRedriveRequest) { r.ScheduledAt = r.ScheduledAt.Add(time.Nanosecond) },
	}
	for i, change := range cases {
		r := valid
		change(&r)
		if id, err := (&IndexDeliveryStore{}).RedriveDiscarded(context.Background(), r); err == nil || id != uuid.Nil {
			t.Fatalf("case %d: %s %v", i, id, err)
		}
	}
}
