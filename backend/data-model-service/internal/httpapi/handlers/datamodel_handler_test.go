package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
)

func TestDataModelHandlerCreateFieldRejectsUnsupportedDataType(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	handler := NewDataModelHandler(
		service.DataModelReadService{},
		service.TableService{},
		service.FieldService{},
		service.FieldEnumValueService{},
		service.LinkService{},
		service.PivotService{},
		service.OptionsService{},
		service.NavigationOptionService{},
	)
	router := gin.New()
	router.POST("/v1/tables/:tableId/fields", handler.CreateField)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/tables/11111111-1111-1111-1111-111111111111/fields",
		bytes.NewBufferString(`{"name":"location","data_type":"coords"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
