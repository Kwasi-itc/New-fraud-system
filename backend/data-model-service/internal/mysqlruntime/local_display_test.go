package mysqlruntime

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalDisplayRoutes(t *testing.T) {
	id, op := uuid.New(), uuid.New()
	calls := 0
	check := func(ctx context.Context, got uuid.UUID) {
		t.Helper()
		calls++
		if localAuthority(ctx, nil) != nil || got != id {
			t.Fatal("lost scope")
		}
	}
	stub := authorWorkflowStub{displayGet: func(c context.Context, i uuid.UUID) (display.Settings, error) {
		check(c, i)
		return display.Settings{TableID: id}, nil
	}, displaySave: func(c context.Context, i uuid.UUID, in display.SaveRequest, k uuid.UUID) (display.Settings, error) {
		check(c, i)
		if k != op || in.Version == nil {
			t.Fatal("lost version/operation")
		}
		return display.Settings{TableID: id}, nil
	}, displayRead: func(c context.Context, i uuid.UUID, in []display.Object) (display.Batch, error) {
		check(c, i)
		if len(in) != 1 || in[0].ID != "a" {
			t.Fatal(in)
		}
		return display.Batch{}, nil
	}}
	secret := strings.Repeat("s", 64)
	router, e := newLocalAuthorHTTP(secret, stub)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/v1/tables/" + id.String() + "/display-settings", "", 200},
		{"PUT", "/v1/tables/" + id.String() + "/display-settings", `{"version":0,"caption_field":""}`, 200},
		{"POST", "/v1/tenants/" + id.String() + "/record-displays", `{"objects":[{"object_type":"records","object_id":"a"}]}`, 200},
		{"PUT", "/v1/tables/" + id.String() + "/display-settings", `{}`, 400},
		{"POST", "/v1/tenants/" + id.String() + "/record-displays", `{"objects":[]}`, 400},
		{"PUT", "/v1/tables/" + id.String() + "/display-settings", `{"version":0,"unknown":true}`, 400},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer "+secret)
		r.Header.Set("Idempotency-Key", op.String())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls != 3 {
		t.Fatal(calls)
	}
}
