package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

type aggregateFactBackfillerStub struct {
	force bool
}

func (s *aggregateFactBackfillerStub) Status(_ context.Context, tenantID uuid.UUID) (ports.AggregateFactBackfillStatus, error) {
	return ports.AggregateFactBackfillStatus{TenantID: tenantID.String(), Ready: true}, nil
}

func (s *aggregateFactBackfillerStub) Backfill(_ context.Context, tenantID uuid.UUID, force bool) (ports.AggregateFactBackfillResult, error) {
	s.force = force
	return ports.AggregateFactBackfillResult{
		TenantID: tenantID.String(), Rebuilt: true,
		Status: ports.AggregateFactBackfillStatus{TenantID: tenantID.String(), Ready: true},
	}, nil
}

func TestAggregateFactBackfillHandlerPassesForce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &aggregateFactBackfillerStub{}
	handler := NewAggregateFactBackfillHandler(stub)
	router := gin.New()
	router.POST("/v1/admin/tenants/:tenantId/aggregate-facts/backfill", handler.Backfill)

	tenantID := "4a63be41-8628-4176-8d70-b8615e671068"
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/"+tenantID+"/aggregate-facts/backfill?force=true", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !stub.force {
		t.Fatal("force was not passed to the backfiller")
	}
}

var _ ports.AggregateFactBackfiller = (*aggregateFactBackfillerStub)(nil)
