package platform

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestFailKeepsDeniedResponseGeneric(t *testing.T) {
	recorder := httptest.NewRecorder()
	server := Server{}

	server.failWithDetail(recorder, ErrDenied, ErrDenied)

	if recorder.Code != 403 {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["error"] != "An active account and required permission are needed." {
		t.Fatalf("unexpected denial response: %q", response["error"])
	}
}

func TestFailWithDetailReturnsLoginErrorOnlyWhenEnabled(t *testing.T) {
	const detail = "OIDC exchange or ID-token validation failed: OIDC ID token verification failed"
	for _, test := range []struct {
		name        string
		debug       bool
		wantMessage string
	}{
		{name: "disabled", wantMessage: "An active account and required permission are needed."},
		{name: "enabled", debug: true, wantMessage: detail},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server := Server{DebugLoginErrors: test.debug}

			server.failWithDetail(recorder, ErrDenied, errors.New(detail))

			if recorder.Code != 403 {
				t.Fatalf("status = %d, want 403", recorder.Code)
			}
			var response map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response["error"] != test.wantMessage {
				t.Fatalf("error = %q, want %q", response["error"], test.wantMessage)
			}
		})
	}
}
