// Package namerecognition implements the optional Marble-compatible text API.
package namerecognition

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

var unavailable = errors.New("name recognition unavailable")

func New(endpoint, token string, timeout time.Duration) screening.NameRecognizer {
	endpoint = strings.TrimSpace(endpoint)
	u, err := url.Parse(endpoint)
	if endpoint == "" || err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
		return nil
	}
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	client := &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, input string) ([]screening.RecognizedName, error) {
		if utf8.RuneCountInString(input) > 1000 {
			return nil, unavailable
		}
		body, _ := json.Marshal(map[string]string{"text": input})
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, unavailable
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.Do(req)
		if err != nil {
			return nil, unavailable
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, unavailable
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 32769))
		if err != nil || len(raw) > 32768 {
			return nil, unavailable
		}
		var matches []screening.RecognizedName
		if json.Unmarshal(raw, &matches) != nil || matches == nil || len(matches) > 20 {
			return nil, unavailable
		}
		for _, m := range matches {
			if strings.TrimSpace(m.Text) == "" || utf8.RuneCountInString(m.Text) > 300 || len(m.Type) > 50 {
				return nil, unavailable
			}
		}
		return matches, nil
	}
}
