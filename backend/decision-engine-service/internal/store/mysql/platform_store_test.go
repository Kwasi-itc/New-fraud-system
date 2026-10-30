package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/google/uuid"
)

func TestPlatformStoreRejectsMissingDependencies(t *testing.T) {
	if _, err := NewPlatformStore(nil, "fraud", uuid.New(), 100, 1024, func(context.Context, *sql.Tx, uuid.UUID, bool) error { return nil }); err == nil {
		t.Fatal("nil pool accepted")
	}
	var store *PlatformStore
	if err := store.Mutate(context.Background(), func(PlatformRepositories) error { t.Fatal("called unconfigured store"); return nil }); err == nil {
		t.Fatal("nil store succeeded")
	}
	if _, err := (RecordTagRepository{}).Create(context.Background(), platform.RecordTag{CreatedAt: time.Now()}); !errors.Is(err, ErrTenantScope) {
		t.Fatal("unconfigured repository did not fail closed", err)
	}
}
