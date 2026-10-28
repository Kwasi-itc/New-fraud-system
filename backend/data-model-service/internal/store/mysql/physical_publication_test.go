package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPhysicalPublicationValidation(t *testing.T) {
	r := PhysicalPublicationRequest{TableID: uuid.New(), Operation: PublicationOperation{ID: uuid.New(), RequestHash: sha256.Sum256([]byte("request")), Condition: PublicationCondition{Mode: PublicationMustBeAbsent}}, ActorKind: "api", ActorID: uuid.NewString()}
	for _, change := range []func(*PhysicalPublicationRequest){
		func(r *PhysicalPublicationRequest) { r.TableID = uuid.Nil },
		func(r *PhysicalPublicationRequest) { r.Operation.ID = uuid.Nil },
		func(r *PhysicalPublicationRequest) { r.Operation.RequestHash = [32]byte{} },
		func(r *PhysicalPublicationRequest) {
			r.Operation.Condition = PublicationCondition{Mode: PublicationAnyVersion}
		},
		func(r *PhysicalPublicationRequest) {
			r.Operation.Condition = PublicationCondition{Mode: PublicationExactVersion}
		},
		func(r *PhysicalPublicationRequest) { r.ActorKind = "unknown" },
		func(r *PhysicalPublicationRequest) { r.ActorID = "" },
		func(r *PhysicalPublicationRequest) { r.ActorID = strings.Repeat("a", 513) },
		func(r *PhysicalPublicationRequest) { r.ActorID = "nul\x00actor" },
		func(r *PhysicalPublicationRequest) { r.ActorID = string([]byte{255}) },
	} {
		invalid := r
		change(&invalid)
		if err := invalid.validate(); err == nil {
			t.Fatal("invalid physical publication accepted")
		}
	}
	e := &PhysicalExecutor{schema: "shared", tenant: uuid.New()}
	pub := &PublicationStore{schema: "shared"}
	deny := errors.New("revoked")
	calls := 0
	authorize := func(context.Context, uuid.UUID, PhysicalPublicationRequest) error { calls++; return deny }
	mutate := func(*sql.Tx) error { t.Fatal("unauthorized mutation"); return nil }
	for _, tc := range []struct {
		pub    *PublicationStore
		auth   func(context.Context, uuid.UUID, PhysicalPublicationRequest) error
		mutate func(*sql.Tx) error
	}{{nil, authorize, mutate}, {&PublicationStore{schema: "other"}, authorize, mutate}, {pub, nil, mutate}, {pub, authorize, nil}} {
		if _, err := e.PublishVerifiedOperation(context.Background(), tc.pub, r, nil, tc.auth, tc.mutate); err == nil {
			t.Fatal("missing boundary accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid construction reached authorization")
	}
	v, err := e.PublishVerifiedOperation(context.Background(), pub, r, nil, authorize, mutate)
	if !errors.Is(err, deny) || v.Version != uuid.Nil || calls != 1 {
		t.Fatalf("authorization not fail-closed: %v %v calls=%d", v, err, calls)
	}
}
