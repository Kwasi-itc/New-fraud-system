package datamodel

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

// ReaderConfig selects a transport explicitly. It does not choose a database
// driver, install schemas, or bypass the application's separate readiness gate.
type ReaderConfig struct {
	Mode                     string
	HTTPURL, HTTPToken       string
	Timeout                  time.Duration
	CacheEntries, CacheBytes int
}

// NewReader is the portable construction boundary for the staged runtime. SQL
// table reads always use the supplied authoritative store. An optional HTTP
// client serves only explicit full-model calls, never SQL failures. With no HTTP
// URL, full-model calls fail explicitly. Existing runtime adoption is separate.
func NewReader(c ReaderConfig, store ports.IngestionContractStore) (ports.DataModelReader, error) {
	if c.Mode != "http" && c.Mode != "sql" {
		return nil, errors.New("explicit data-model reader mode must be http or sql")
	}
	if c.CacheEntries < 0 || c.CacheEntries > 10000 || c.CacheBytes < 0 || c.CacheBytes > 64<<20 || (c.CacheEntries == 0) != (c.CacheBytes == 0) {
		return nil, errors.New("cache requires paired zero limits or bounded positive entries/bytes")
	}
	if c.Mode == "sql" && store == nil {
		return nil, errors.New("SQL reader requires an authoritative contract store")
	}
	var full ports.DataModelReader
	if c.HTTPURL != "" {
		u, err := url.Parse(c.HTTPURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("data-model HTTP URL must be an http(s) service base without credentials, query or fragment")
		}
		if strings.TrimSpace(c.HTTPToken) == "" || strings.ContainsAny(c.HTTPToken, "\r\n") {
			return nil, errors.New("private data-model HTTP credential required")
		}
		if c.Timeout <= 0 || c.Timeout > 30*time.Minute {
			return nil, errors.New("bounded positive data-model HTTP timeout required")
		}
		full = NewHTTPClientWithCacheLimits(c.HTTPURL, c.Timeout, c.CacheEntries, c.CacheBytes).WithAuthToken(c.HTTPToken)
	} else if c.Mode == "http" {
		return nil, errors.New("HTTP reader requires a service URL")
	}
	if c.Mode == "http" {
		return full, nil
	}
	return NewContractReader(store, full, c.CacheEntries, c.CacheBytes), nil
}
