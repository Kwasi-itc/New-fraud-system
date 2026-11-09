package mysqlruntime

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/gin-gonic/gin"
	driver "github.com/go-sql-driver/mysql"
)

func decodeLocalPatch(c *gin.Context, value any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("single JSON object required")
	}
	return nil
}

func localUpdateError(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "update_unavailable"
	if errors.Is(err, sql.ErrNoRows) {
		status, code = 404, "not_found"
	}
	if errors.Is(err, store.ErrPhysicalConflict) || errors.Is(err, store.ErrPublicationOperationConflict) || errors.Is(err, contract.ErrPublicationVersionConflict) {
		status, code = 409, "update_conflict"
	}
	if errors.Is(err, store.ErrPhysicalPrecondition) {
		status, code = 409, "explicit_backfill_required"
	}
	if errors.Is(err, service.ErrFieldPatchInvalid) || errors.Is(err, service.ErrRelationshipInvalid) {
		status, code = 400, err.Error()
	}
	if errors.Is(err, store.ErrFieldDeletionDependency) || errors.Is(err, service.ErrFieldDependency) || errors.Is(err, service.ErrFieldDuplicate) || errors.Is(err, store.ErrPhysicalTextDuplicate) {
		status, code = 409, err.Error()
	}
	if errors.Is(err, store.ErrPhysicalPlanningUnsupported) {
		status, code = 422, err.Error()
	}
	if errors.Is(err, store.ErrPhysicalNullValues) {
		status, code = 409, err.Error()
	}
	var invalidDisplay *display.Invalid
	if errors.As(err, &invalidDisplay) {
		status, code = 400, invalidDisplay.Error()
	}
	if errors.Is(err, display.ErrConflict) {
		status, code = 409, "display_conflict"
	}
	var mysqlError *driver.MySQLError
	if errors.Is(err, mysqlstore.ErrDDLLockBusy) {
		code = "schema_change_busy"
		c.Header("Retry-After", "1")
	}
	if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
		status, code = 409, "duplicate_name_or_value"
	}
	slog.ErrorContext(c.Request.Context(), "local presentation update failed", "error", err)
	c.JSON(status, gin.H{"error": code})
}
