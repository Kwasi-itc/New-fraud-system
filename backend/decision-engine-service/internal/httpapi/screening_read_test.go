package httpapi

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestScreeningReadPrivateBoundary(t *testing.T) {
	// A lazy pool: all requests must fail before any database access.
	db, err := pgxpool.New(context.Background(), "postgres://unused@127.0.0.1:1/unused?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	token := strings.Repeat("s", 32)
	router := NewRouter(slog.Default(), db, RouterConfig{AuthMode: "disabled", ScreeningAuthoringToken: token})
	path := "/internal/screening-authoring/tenants/11111111-1111-4111-8111-111111111111/decisions/44444444-4444-4444-8444-444444444444/screenings"
	for _, tc := range []struct {
		token, user, session, version string
		want                          int
	}{
		{"", "", "", "", 401},
		{"wrong", "", "", "", 401},
		{token, "", "", "", 400},
		{token, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "0", 403},
	} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("X-Screening-User", tc.user)
		req.Header.Set("X-Screening-Session", tc.session)
		req.Header.Set("X-Screening-Version", tc.version)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("got %d want %d: %s", w.Code, tc.want, w.Body.String())
		}
	}
}
