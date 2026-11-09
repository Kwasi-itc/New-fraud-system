package workflowreceipt

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "workflow-test-token-at-least-32-characters"

func testRequest() ports.DispatchRequest {
	return ports.DispatchRequest{Kind: ports.JobWorkflow, Key: "original-key", PayloadSHA256: "original-hash", Payload: []byte(`{"action_type":"create_case"}`)}
}
func TestWorkflowReceiptHTTPContract(t *testing.T) {
	for _, test := range []struct {
		name          string
		status        int
		body          string
		reconcile, ok bool
	}{
		{"accepted", 200, `{"idempotency_key":"original-key","payload_sha256":"original-hash","outcome":"accepted","reference":"case-id"}`, false, true},
		{"missing", 200, `{"idempotency_key":"original-key","payload_sha256":"original-hash","outcome":"not_found","reference":""}`, true, true},
		{"missing_not_delivery_success", 200, `{"idempotency_key":"original-key","payload_sha256":"original-hash","outcome":"not_found","reference":""}`, false, false},
		{"legacy_success", 200, `{"case":{"id":"case-id"}}`, false, false},
		{"empty_204", 204, "", false, false},
		{"wrong_echo", 200, `{"idempotency_key":"different","payload_sha256":"original-hash","outcome":"accepted","reference":"case-id"}`, false, false},
		{"non_2xx_is_uncertain", 409, `{"outcome":"rejected"}`, false, false},
		{"multiple_json_values", 200, `{"idempotency_key":"original-key","payload_sha256":"original-hash","outcome":"accepted","reference":"case-id"} {}`, false, false},
		{"oversized", 200, strings.Repeat(" ", 16385), false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				operation := "deliver"
				if test.reconcile {
					operation = "reconcile"
				}
				if r.Method != "POST" || r.URL.Path != "/internal/v1/workflow-receipts/"+operation || r.Header.Get("Authorization") != "Bearer "+testToken {
					t.Error("wrong configured destination or credential")
				}
				var body ports.DispatchRequest
				if json.NewDecoder(r.Body).Decode(&body) != nil || body.Key != "original-key" || body.PayloadSHA256 != "original-hash" {
					t.Error("changed request identity")
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL, Token: testToken, Timeout: time.Second, AllowHTTP: true})
			if err != nil {
				t.Fatal(err)
			}
			var receipt ports.DispatchReceipt
			if test.reconcile {
				receipt, err = client.Reconcile(context.Background(), testRequest())
			} else {
				receipt, err = client.Deliver(context.Background(), testRequest())
			}
			if (err == nil) != test.ok {
				t.Fatal(receipt, err)
			}
			if !test.ok && !errors.Is(err, ErrUnavailable) {
				t.Fatal("unredacted/ambiguous error", err)
			}
		})
	}
}
func TestWorkflowReceiptDestinationPolicy(t *testing.T) {
	var received atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { received.Add(1) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	client, err := New(Config{BaseURL: redirect.URL, Token: testToken, Timeout: time.Second, AllowHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Deliver(context.Background(), testRequest()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if received.Load() != 0 {
		t.Fatal("redirect disclosed credential")
	}
	invalid := testRequest()
	invalid.Payload = []byte(`{"action_type":"emit_event","action_config":{"url":"` + destination.URL + `"}}`)
	if _, err := client.Deliver(context.Background(), invalid); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if received.Load() != 0 {
		t.Fatal("action URL was followed")
	}
	for _, config := range []Config{{BaseURL: destination.URL, Token: testToken, Timeout: time.Second}, {BaseURL: "https://user:password@example.test", Token: testToken, Timeout: time.Second}, {BaseURL: "https://example.test/other", Token: testToken, Timeout: time.Second}, {BaseURL: "https://example.test", Token: "short", Timeout: time.Second}} {
		if _, err := New(config); err == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Deliver(canceled, testRequest()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
