package eventstore

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestClickHouseSelectCarriesServerCancellationGuards(t *testing.T) {
	var received map[string]string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		received = map[string]string{
			"cancel":  r.URL.Query().Get("cancel_http_readonly_queries_on_client_close"),
			"timeout": r.URL.Query().Get("max_execution_time"),
			"speed":   r.URL.Query().Get("timeout_before_checking_execution_speed"),
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("1\n"))}, nil
	})

	client := newClickHouseClient(Config{
		ClickHouseURL: "http://clickhouse.test", ClickHouseDatabase: "fraud_events", HTTPTimeout: 30 * time.Second,
	})
	client.client.Transport = transport
	if _, err := client.execute(context.Background(), "SELECT 1", nil); err != nil {
		t.Fatal(err)
	}
	if received["cancel"] != "1" || received["timeout"] != "29.000" || received["speed"] != "0" {
		t.Fatalf("SELECT safeguards = %#v", received)
	}
}

func TestClickHouseInsertDoesNotUseReadonlyCancellationSetting(t *testing.T) {
	var cancel string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		cancel = r.URL.Query().Get("cancel_http_readonly_queries_on_client_close")
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("\n"))}, nil
	})

	client := newClickHouseClient(Config{
		ClickHouseURL: "http://clickhouse.test", ClickHouseDatabase: "fraud_events", HTTPTimeout: 30 * time.Second,
	})
	client.client.Transport = transport
	if _, err := client.execute(context.Background(), "INSERT INTO events FORMAT JSONEachRow", nil); err != nil {
		t.Fatal(err)
	}
	if cancel != "" {
		t.Fatalf("INSERT unexpectedly set readonly cancellation to %q", cancel)
	}
}
