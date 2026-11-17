package sourceconnectors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const MaxArtifactBytes int64 = 64 * 1024 * 1024

type Download struct {
	Path         string
	SHA256       string
	Bytes        int64
	ETag         string
	LastModified string
	NotModified  bool
	RetryAfter   time.Duration
}
type Fetcher struct{ Client *http.Client }

func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified()
}
func secureDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrDownload
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, ErrDownload
	}
	for _, ip := range ips {
		if !publicAddress(ip) {
			return nil, ErrDownload
		}
	}
	dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	for _, ip := range ips {
		conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if e == nil {
			return conn, nil
		}
	}
	return nil, ErrDownload
}
func NewFetcher() Fetcher {
	transport := &http.Transport{DialContext: secureDial, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second, MaxIdleConns: 4, MaxIdleConnsPerHost: 2, MaxConnsPerHost: 2, IdleConnTimeout: 60 * time.Second}
	return Fetcher{Client: &http.Client{Transport: transport, Timeout: 90 * time.Second}}
}
func (s Spec) allowed(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Fragment == "" && (u.Port() == "" || u.Port() == "443") && s.hosts[strings.ToLower(u.Hostname())]
}

// Fetch follows only approved source-specific HTTPS hosts. It rejects partial
// files, HTML/error responses, private DNS destinations and decompressed overflow.
// Tokens/redirect query strings are never included in returned diagnostics.
func (f Fetcher) Fetch(ctx context.Context, s Spec, dir, etag, lastModified string) (out Download, resultErr error) {
	if f.Client == nil || !s.allowed(s.URL) {
		return out, ErrDownload
	}
	client := *f.Client
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		r.Header.Del("Referer")
		r.Header.Del("Authorization")
		r.Header.Del("Cookie")
		if len(via) >= 4 || !s.allowed(r.URL.String()) {
			return ErrDownload
		}
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return out, ErrDownload
	}
	request.Header.Set("User-Agent", "ITC-Screening-Source-Collector/1.0")
	request.Header.Set("Accept", "application/xml,text/xml,application/octet-stream")
	if len(etag) <= 1024 && etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	if len(lastModified) <= 100 && lastModified != "" {
		request.Header.Set("If-Modified-Since", lastModified)
	}
	response, err := client.Do(request)
	if err != nil {
		return out, ErrDownload
	}
	defer response.Body.Close()
	out.ETag = response.Header.Get("ETag")
	out.LastModified = response.Header.Get("Last-Modified")
	if len(out.ETag) > 1024 || len(out.LastModified) > 100 {
		return Download{}, ErrDownload
	}
	if response.StatusCode == 304 {
		out.NotModified = true
		return out, nil
	}
	if response.StatusCode == 429 || response.StatusCode == 503 {
		if seconds, e := strconv.Atoi(response.Header.Get("Retry-After")); e == nil && seconds >= 0 {
			out.RetryAfter = min(time.Duration(seconds)*time.Second, time.Hour)
		} else if when, e := http.ParseTime(response.Header.Get("Retry-After")); e == nil {
			out.RetryAfter = min(max(time.Until(when), 0), time.Hour)
		}
		return out, ErrDownload
	}
	if response.StatusCode != 200 || response.Header.Get("Content-Range") != "" || response.ContentLength > MaxArtifactBytes || strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/html") {
		return out, ErrDownload
	}
	file, err := os.CreateTemp(dir, "official-source-*.xml")
	if err != nil {
		return out, ErrDownload
	}
	out.Path = file.Name()
	defer func() {
		file.Close()
		if resultErr != nil {
			os.Remove(out.Path)
		}
	}()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, MaxArtifactBytes+1))
	if err != nil {
		return out, ErrDownload
	}
	if size == 0 || size > MaxArtifactBytes {
		return out, ErrLimit
	}
	if response.ContentLength >= 0 && !response.Uncompressed && size != response.ContentLength {
		return out, ErrDownload
	}
	if err = file.Sync(); err != nil {
		return out, ErrDownload
	}
	if err = file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return out, ErrDownload
	}
	out.Bytes = size
	out.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return out, nil
}
