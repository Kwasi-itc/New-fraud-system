package mysqlruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/mysql"
	"github.com/google/uuid"
)

func TestPublisherContract(t *testing.T) {
	id := uuid.New()
	d := store.Delivery{ID: id, TenantID: uuid.New(), AggregateType: "case", AggregateID: uuid.NewString(), EventType: "comment_added", CreatedAt: time.Now().UTC(), Payload: json.RawMessage(`{"comment":"test evidence"}`)}
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Idempotency-Key") != id.String() || r.Header.Get("Authorization") != "Bearer private-fixture-token" {
			t.Error("missing publication identity")
		}
		var got store.Delivery
		if e := json.NewDecoder(r.Body).Decode(&got); e != nil {
			t.Error(e)
		}
		if got.ID != id || got.AggregateID != d.AggregateID {
			t.Error("publication payload mismatch")
		}
		w.WriteHeader(204)
	}))
	defer target.Close()
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for range 2 {
		if failure := publish(context.Background(), client, target.URL, "private-fixture-token", d); failure != "" {
			t.Fatal(failure)
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer redirect.Close()
	if publish(context.Background(), client, redirect.URL, "private-fixture-token", d) == "" || calls != 2 {
		t.Fatal("redirect followed or treated as delivery")
	}
}
