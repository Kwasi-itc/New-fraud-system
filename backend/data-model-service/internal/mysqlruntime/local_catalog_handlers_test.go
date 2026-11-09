package mysqlruntime

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCatalogMutationHandlers(t *testing.T) {
	id, op := uuid.New(), uuid.New()
	calls := 0
	check := func(ctx context.Context, got, key uuid.UUID) {
		t.Helper()
		calls++
		if got != id || key != op || localAuthority(ctx, nil) != nil {
			t.Fatal("lost authority or identity")
		}
	}
	worker := authorWorkflowStub{
		fieldDelete: func(ctx context.Context, got uuid.UUID, dry bool, key uuid.UUID) (datamodel.DeleteReport, error) {
			check(ctx, got, key)
			if !dry {
				t.Fatal("lost dry run")
			}
			return datamodel.NewDeleteReport(), nil
		},
		enumMutation: func(ctx context.Context, input service.UpdateFieldEnumValueInput, remove bool, key uuid.UUID) (datamodel.FieldEnumValue, error) {
			check(ctx, input.EnumValueID, key)
			if !remove && (input.Label == nil || *input.Label != "New") {
				t.Fatal("lost patch")
			}
			return datamodel.FieldEnumValue{ID: id}, nil
		},
	}
	secret := strings.Repeat("s", 64)
	r, err := newLocalAuthorHTTP(secret, worker)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"DELETE", "/v1/fields/" + id.String() + "?dry_run=true", "", 200},
		{"PATCH", "/v1/enum-values/" + id.String(), `{"label":"New"}`, 200},
		{"DELETE", "/v1/enum-values/" + id.String(), "", 204},
		{"PATCH", "/v1/enum-values/" + id.String(), `{}`, 400},
		{"PATCH", "/v1/enum-values/" + id.String(), `{"unknown":true}`, 400},
		{"DELETE", "/v1/fields/" + id.String() + "?dry_run=oops", "", 400},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("Idempotency-Key", op.String())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls != 3 {
		t.Fatal(calls)
	}
}
