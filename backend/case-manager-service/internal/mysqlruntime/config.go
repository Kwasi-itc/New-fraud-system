// Package mysqlruntime composes the local MySQL case API and maintenance loop.
package mysqlruntime

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type Config struct {
	Address, Database, AccountHost, Issuer, Audience, KeyID string
	ServiceTenants                                          []uuid.UUID
	PublisherURL                                            string
	ServiceTenantMode                                       string
}

func Load(path string) (Config, error) {
	var c Config
	f, e := os.Open(path)
	if e != nil {
		return c, e
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("one bounded case configuration required")
	}
	if c.Address != "mysql:3306" || c.Database != "fraud" || c.Issuer == "" || c.Audience == "" || c.KeyID == "" || len(c.ServiceTenants) > 1000 {
		return c, errors.New("prepared local case runtime required")
	}
	if c.ServiceTenantMode != "" && c.ServiceTenantMode != "registered" && c.ServiceTenantMode != "allowlist" {
		return c, errors.New("invalid service tenant mode")
	}
	if c.ServiceTenantMode == "registered" && len(c.ServiceTenants) != 0 {
		return c, errors.New("registered tenant mode cannot carry an allowlist")
	}
	if c.PublisherURL != "" {
		u, e := url.Parse(c.PublisherURL)
		if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" {
			return c, errors.New("publisher must be an absolute HTTP(S) URL without credentials")
		}
	}
	return c, nil
}
func Secret(name string) (string, error) {
	b, e := os.ReadFile("/run/secrets/" + name)
	if e != nil {
		return "", e
	}
	s := string(b)
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(s) {
		return "", errors.New("invalid prepared case secret")
	}
	return s, nil
}
func LocalEnabled() bool { return strings.TrimSpace(os.Getenv("CASE_LOCAL_MYSQL")) == "1" }
