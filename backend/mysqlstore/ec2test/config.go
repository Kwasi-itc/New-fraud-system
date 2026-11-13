// Package ec2test implements the explicit HTTP EC2/Aurora test deployment.
// It is separate from production activation and the loopback development profile.
package ec2test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
)

const Environment = "EC2_MYSQL_TEST_CONFIG"

type Config struct {
	Address, AdminUser, CAFile, AccountHost, ReferencePasswordFile string
	Origins                                                        []string
	Gateway                                                        bool
}

func Enabled() bool { return os.Getenv(Environment) != "" }

func Load() (Config, error) {
	var c Config
	f, err := os.Open(os.Getenv(Environment))
	if err != nil {
		return c, errors.New("EC2 test configuration is unavailable")
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, errors.New("invalid EC2 test configuration")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("one EC2 test configuration required")
	}
	host, port, err := net.SplitHostPort(c.Address)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || number < 1 || number > 65535 || net.ParseIP(host) != nil || !strings.HasSuffix(host, ".rds.amazonaws.com") {
		return c, errors.New("an Aurora DNS writer endpoint and valid port are required")
	}
	if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`).MatchString(c.AdminUser) || c.CAFile == "" {
		return c, errors.New("explicit migration user and CA file required")
	}
	parts := strings.Split(c.AccountHost, "/")
	if len(parts) != 2 || net.ParseIP(parts[0]).To4() == nil || parts[1] != "255.255.255.255" {
		return c, errors.New("explicit single EC2 client IPv4 address/32 netmask required")
	}
	if len(c.Origins) < 1 || len(c.Origins) > 16 {
		return c, errors.New("bounded HTTP test origin list required")
	}
	for _, origin := range c.Origins {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			return c, errors.New("HTTP test origins must be exact IP origins")
		}
	}
	return c, nil
}

func (c Config) tls() (*tls.Config, error) {
	data, err := os.ReadFile(c.CAFile)
	if err != nil {
		return nil, errors.New("Aurora CA file is unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(data) {
		return nil, errors.New("Aurora CA file contains no certificates")
	}
	host, _, _ := net.SplitHostPort(c.Address)
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: host}, nil
}

// Open preserves ordinary runtime behavior unless the explicit test profile is
// configured. All Aurora runtime connections require certificate/host verification.
func Open(ctx context.Context, c mysqlstore.Config) (*sql.DB, error) {
	if Enabled() {
		profile, err := Load()
		if err != nil {
			return nil, err
		}
		c.Address = profile.Address
		c.TLS, err = profile.tls()
		if err != nil {
			return nil, err
		}
		c.AllowInsecureTransport = false
	}
	return mysqlstore.Open(ctx, c)
}

// Driver adapts legacy owner/DDL connectors explicitly. Reference capture always
// stays on the independent disposable reference server, never the Aurora target.
func Driver(c *driver.Config, reference bool) error {
	if !Enabled() {
		return nil
	}
	profile, err := Load()
	if err != nil {
		return err
	}
	c.Net = "tcp"
	if reference {
		c.Addr = "mysql-reference:3306"
		password, err := os.ReadFile(profile.ReferencePasswordFile)
		if err != nil || len(password) < 32 {
			return errors.New("independent reference credential is unavailable")
		}
		c.Passwd = string(password)
		return nil
	}
	c.Addr = profile.Address
	if c.User == "root" {
		c.User = profile.AdminUser
	}
	c.TLS, err = profile.tls()
	return err
}

func VersionSupported(version string) bool {
	if !Enabled() {
		return version == "8.4.8"
	}
	return regexp.MustCompile(`^8\.4\.[0-9]+(?:[-.].*)?$`).MatchString(version)
}

// InstallerHost retains existing local behavior and propagates configuration
// errors rather than silently granting a wildcard account on a remote server.
func InstallerHost() (string, error) {
	if !Enabled() {
		return "localhost", nil
	}
	c, err := Load()
	return strings.Split(c.AccountHost, "/")[0], err
}

func Origin(raw string) (string, error) {
	c, err := Load()
	if err != nil {
		return "", err
	}
	for _, allowed := range c.Origins {
		if raw == allowed {
			return raw, nil
		}
	}
	return "", errors.New("HTTP origin is not in the EC2 test deployment")
}

// RequirePresented preserves user and machine credentials for an owner router
// that validates them itself. It never injects the local operator credential.
func RequirePresented(next http.Handler, origin string) (http.Handler, error) {
	if _, err := Origin(origin); err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if supplied := r.Header.Get("Origin"); supplied != "" && supplied != origin {
			http.Error(w, "origin denied", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/internal/") {
			values := r.Header.Values("Authorization")
			if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") || len(values[0]) <= 7 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	}), nil
}
