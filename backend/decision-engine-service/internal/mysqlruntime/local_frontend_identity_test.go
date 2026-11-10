package mysqlruntime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalFrontendPreservesPresentedSession(t *testing.T) {
	machine := "Bearer " + strings.Repeat("a", 64)
	session := "Bearer " + strings.Repeat("s", 43)
	seen := ""
	handler, err := localFrontendHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		if seen != machine && seen != session {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(204)
	}), "1", "http://localhost:8082", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		credential, origin string
		status             int
		want               string
	}{
		{"", "", 204, machine}, {session, "", 204, session}, {"Bearer invalid-session", "", 401, "Bearer invalid-session"}, {session, "https://foreign.example", 403, ""},
	} {
		seen = ""
		request := httptest.NewRequest("GET", "/v1/rule-functions", nil)
		if test.credential != "" {
			request.Header.Set("Authorization", test.credential)
		}
		if test.origin != "" {
			request.Header.Set("Origin", test.origin)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status || seen != test.want {
			t.Fatalf("credential boundary: status=%d expected=%d called=%t", response.Code, test.status, seen != "")
		}
	}
	request := httptest.NewRequest("GET", "/v1/rule-functions", nil)
	request.Header.Add("Authorization", machine)
	request.Header.Add("Authorization", session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 401 {
		t.Fatal("ambiguous credentials were accepted")
	}
}
