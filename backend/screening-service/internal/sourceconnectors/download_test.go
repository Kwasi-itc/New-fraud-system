package sourceconnectors

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOfficialDownloaderBoundsAndConditionalResponses(t *testing.T) {
	spec, _ := Lookup("ofac-sdn")
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		header    http.Header
		wantError bool
	}{{"complete", 200, "<xml/>", http.Header{}, false}, {"partial", 206, "<xml/>", http.Header{}, true}, {"html", 200, "oops", http.Header{"Content-Type": []string{"text/html"}}, true}, {"retry", 429, "", http.Header{"Retry-After": []string{"20"}}, true}, {"unchanged", 304, "", http.Header{}, false}} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("If-None-Match") != "fixture-etag" || r.Header.Get("User-Agent") == "" {
					t.Fatal("missing bounded source headers")
				}
				return &http.Response{StatusCode: tc.status, Header: tc.header, Body: io.NopCloser(strings.NewReader(tc.body)), ContentLength: int64(len(tc.body)), Request: r}, nil
			})}
			got, err := (Fetcher{Client: client}).Fetch(context.Background(), spec, t.TempDir(), "fixture-etag", "")
			if (err != nil) != tc.wantError {
				t.Fatal(got, err)
			}
			if tc.status == 200 && !tc.wantError {
				raw, e := os.ReadFile(got.Path)
				if e != nil || string(raw) != tc.body || got.SHA256 == "" {
					t.Fatal(got, e)
				}
			}
			if tc.status == 304 && !got.NotModified {
				t.Fatal(got)
			}
			if tc.status == 429 && got.RetryAfter.Seconds() != 20 {
				t.Fatal(got)
			}
		})
	}
	// A redirect cannot leave the exact source-specific allowlist or send tokens
	// to an arbitrary host, localhost, HTTP endpoint or credential-bearing URL.
	for _, raw := range []string{"http://sanctionslistservice.ofac.treas.gov/x", "https://127.0.0.1/x", "https://sanctionslistservice.ofac.treas.gov.evil.invalid/x", "https://user:pass@sanctionslistservice.ofac.treas.gov/x", "https://sanctionslistservice.ofac.treas.gov:444/x"} {
		if spec.allowed(raw) {
			t.Fatal("unsafe URL accepted", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1"} {
		if publicAddress(netip.MustParseAddr(raw)) {
			t.Fatal("private destination accepted", raw)
		}
	}
}
