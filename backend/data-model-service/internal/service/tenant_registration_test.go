package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

type registrationBoundaryIDs struct {
	id    uuid.UUID
	calls int
}

func (g *registrationBoundaryIDs) New() uuid.UUID { g.calls++; return g.id }

type registrationBoundaryClock struct{}

func (registrationBoundaryClock) Now() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

type registrationBoundaryManager struct {
	scope uuid.UUID
	calls int
	err   error
}

func (m *registrationBoundaryManager) RunRegistration(_ context.Context, id uuid.UUID, _ func(ports.MutationStore) error) error {
	m.scope = id
	m.calls++
	return m.err
}

func TestTenantRegistrationScopeAllocation(t *testing.T) {
	id := uuid.New()
	ids := &registrationBoundaryIDs{id: id}
	denied := errors.New("registration denied")
	manager := &registrationBoundaryManager{err: denied}
	base := NewTenantService(nil, nil, nil, nil, ids, registrationBoundaryClock{})
	s, err := base.WithTenantRegistrationManager(manager)
	if err != nil {
		t.Fatal(err)
	}
	if record, err := s.Create(context.Background(), tenant.CreateInput{Name: "New tenant"}); !errors.Is(err, denied) || record.ID != uuid.Nil || ids.calls != 1 || manager.scope != id || manager.calls != 1 {
		t.Fatalf("allocation or denial: %+v %v", record, err)
	}
	ids.id = uuid.Nil
	if _, err := s.Create(context.Background(), tenant.CreateInput{Name: "New tenant"}); err == nil || manager.calls != 1 {
		t.Fatal("zero identity reached manager")
	}
	before := ids.calls
	if _, err := s.Create(context.Background(), tenant.CreateInput{}); err == nil || ids.calls != before {
		t.Fatal("invalid input allocated identity")
	}
	if _, err := base.WithTenantRegistrationManager(nil); err == nil {
		t.Fatal("nil registration manager accepted")
	}
	if _, err := base.Create(context.Background(), tenant.CreateInput{Name: "New tenant"}); err == nil {
		t.Fatal("base service was mutated")
	}
}
