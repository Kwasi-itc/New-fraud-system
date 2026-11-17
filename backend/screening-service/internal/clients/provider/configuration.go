package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

// ValidateConfiguration runs at API/worker startup. A malformed map is never
// silently normalized into a different effective provider configuration.
func ValidateConfiguration(defaultURL, raw, openURL string) error {
	validURL := func(value string) bool {
		u, e := url.Parse(value)
		return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.Fragment == "" && u.RawQuery == ""
	}
	for _, value := range []string{defaultURL, openURL} {
		if value != "" && !validURL(value) {
			return fmt.Errorf("invalid provider endpoint configuration")
		}
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := map[string]bool{}
	key := regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)
	validate := func(k, v string) error {
		if !key.MatchString(k) || seen[k] || k == "default" || k == "synthetic" || k == "internal-watchlists" || !validURL(v) {
			return fmt.Errorf("invalid or duplicate provider registry entry")
		}
		seen[k] = true
		return nil
	}
	if strings.HasPrefix(strings.TrimSpace(raw), "{") {
		d := json.NewDecoder(strings.NewReader(raw))
		token, e := d.Token()
		if e != nil || token != json.Delim('{') {
			return fmt.Errorf("invalid provider registry JSON")
		}
		for d.More() {
			token, e = d.Token()
			if e != nil {
				return fmt.Errorf("invalid provider registry JSON")
			}
			k, ok := token.(string)
			if !ok {
				return fmt.Errorf("invalid provider registry key")
			}
			var v string
			if d.Decode(&v) != nil {
				return fmt.Errorf("invalid provider registry value")
			}
			if e = validate(k, v); e != nil {
				return e
			}
		}
		if _, e = d.Token(); e != nil {
			return fmt.Errorf("invalid provider registry JSON")
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			return fmt.Errorf("invalid provider registry trailing data")
		}
	} else {
		for _, entry := range strings.Split(raw, ",") {
			k, v, ok := strings.Cut(entry, "=")
			if !ok {
				return fmt.Errorf("invalid provider registry entry")
			}
			if e := validate(strings.TrimSpace(k), strings.TrimSpace(v)); e != nil {
				return e
			}
		}
	}
	return nil
}
