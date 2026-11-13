package namerecognition

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRecognitionContract(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		ok     bool
	}{{200, `[{"type":"Person","text":"Synthetic"}]`, true}, {200, `[]`, true}, {200, `null`, false}, {200, `{}`, false}, {200, `[{"type":"Person","text":""}]`, false}, {429, "secret", false}, {401, "secret", false}, {503, "secret", false}, {302, "", false}, {200, strings.Repeat("x", 32769), false}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
				t.Error("wrong request")
			}
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["text"] != "Synthetic input" {
				t.Error(body)
			}
			w.Header().Set("Location", "http://private.invalid")
			w.WriteHeader(tc.status)
			w.Write([]byte(tc.body))
		}))
		got, err := New(server.URL, "synthetic-token", time.Second)(context.Background(), "Synthetic input")
		server.Close()
		if (err == nil) != tc.ok {
			t.Fatal(got, err)
		}
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Fatal("leaked response")
		}
	}
	if New("http://remote.invalid", "", time.Second) != nil || New("https://user:secret@remote.invalid", "", time.Second) != nil || New("https://remote.invalid?key=secret", "", time.Second) != nil {
		t.Fatal("unsafe endpoint accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte(`[]`))
	}))
	defer server.Close()
	if _, err := New(server.URL, "", 20*time.Millisecond)(context.Background(), "Synthetic"); err == nil {
		t.Fatal("timeout accepted")
	}
}
