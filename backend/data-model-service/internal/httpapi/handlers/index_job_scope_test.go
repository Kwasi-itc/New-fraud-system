package handlers

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http/httptest"
	"testing"
)

type scopedJobRepository struct {
	ports.IndexJobRepository
	calls         int
	tenant, table uuid.UUID
}

func (r *scopedJobRepository) ListByTables(_ context.Context, tenant uuid.UUID, tables []uuid.UUID, limit int) ([]datamodel.IndexJob, error) {
	r.calls++
	if tenant != r.tenant || len(tables) != 1 || tables[0] != r.table || limit != 2 {
		return nil, datamodel.ErrIndexJobScopeLimit
	}
	return []datamodel.IndexJob{}, nil
}
func TestScopedIndexJobHandler(t *testing.T) {
	r := &scopedJobRepository{tenant: uuid.New(), table: uuid.New()}
	s := service.NewIndexJobService(nil, nil, nil, r, nil, nil, nil, nil)
	h := NewIndexJobHandler(s)
	router := gin.New()
	router.GET("/tenants/:tenantId/jobs", h.ListScoped)
	base := "/tenants/" + r.tenant.String() + "/jobs"
	valid := "?table_id=" + r.table.String() + "&limit=2"
	for _, test := range []struct {
		query  string
		status int
	}{{valid, 200}, {"", 400}, {valid + "&limit=3", 400}, {valid + "&actor=forged", 400}, {"?table_id=bad&limit=2", 400}, {"?table_id=" + r.table.String() + "&limit=0", 400}, {valid + "&table_id=" + r.table.String(), 400}} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", base+test.query, nil))
		if rec.Code != test.status {
			t.Fatal(test, rec.Code, rec.Body.String())
		}
	}
	if r.calls != 1 {
		t.Fatal("invalid requests reached owner", r.calls)
	}
}
