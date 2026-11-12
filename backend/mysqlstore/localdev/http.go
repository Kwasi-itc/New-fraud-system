// Package localdev contains explicit opt-in adapters for the loopback-only
// MySQL development stack. Normal service entrypoints do not select this mode.
package localdev

import (
	"crypto/subtle"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"net/http"
	"net/url"
	"strings"
)

// Origin requires loopback unless the separate EC2 test configuration is
// explicitly selected. A configured URL alone is not a network fence.
func Origin(enabled, raw string) (string, error) {
	if ec2test.Enabled() {
		if enabled != "1" {
			return "", errors.New("explicit test compatibility switch required")
		}
		return ec2test.Origin(raw)
	}
	u, err := url.Parse(raw)
	if enabled != "1" || err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("explicit local MySQL mode and loopback HTTP origin required")
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
	default:
		return "", errors.New("local MySQL origin must be loopback")
	}
	return u.Scheme + "://" + u.Host, nil
}

// WithoutLogin preserves the underlying owner service credential contract while
// removing login from the local HTTP boundary. The credential is server-owned;
// headers supplied by callers cannot choose another principal. Tenant checks
// still belong to the scoped owner repositories. It rejects foreign browser
// origins, including writes that would otherwise reach the token-auth router.
func WithoutLogin(next http.Handler, enabled, rawOrigin, credential string) (http.Handler, error) {
	origin, err := Origin(enabled, rawOrigin)
	if err != nil {
		return nil, err
	}
	if next == nil || len(credential) < 16 {
		return nil, errors.New("local handler and internal service credential required")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if supplied := r.Header.Get("Origin"); supplied != "" && supplied != origin {
			http.Error(w, "foreign origin is unavailable in local MySQL mode", http.StatusForbidden)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "cross-site requests are unavailable in local MySQL mode", http.StatusForbidden)
			return
		}
		clone := r.Clone(r.Context())
		if ec2test.Enabled() {
			// Remote tests retain the service credential check; no anonymous
			// request is promoted to the local operator identity.
			if strings.HasPrefix(r.URL.Path, "/v1/") && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+credential)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		} else {
			clone.Header.Set("Authorization", "Bearer "+credential)
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, clone)
	}), nil
}
