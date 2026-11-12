package mysqlauth

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/google/uuid"
)

type LocalConfig struct {
	Address          string
	Database         string
	AppOrigin        string
	InvitationOrigin string
	AccountHost      string
	Issuer           string
	Audience         string
	KeyID            string
	DefaultTenantID  uuid.UUID
	Provider         oidcprovider.Config
	SMTP             platform.SMTPMailer
}

func LoadConfig(path string) (LocalConfig, error) {
	var c LocalConfig
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
		return c, errors.New("one auth config required")
	}
	if os.Getenv("AUTH_LOCAL_MYSQL") != "1" {
		return c, errors.New("explicit local MySQL runtime required")
	}
	if _, e = localdev.Origin("1", c.AppOrigin); e != nil {
		return c, e
	}
	if c.Database != "fraud" && !(os.Getenv("AUTH_REQUIRE_MYSQL_TEST") == "1" && strings.HasPrefix(c.Database, "auth_test_")) {
		return c, errors.New("shared fraud database required")
	}
	if c.Issuer == "" || c.Audience == "" || c.KeyID == "" {
		return c, errors.New("platform JWT settings required")
	}
	providerConfig := c.Provider
	if providerConfig.ClientSecret != "" {
		return c, errors.New("OIDC secret belongs in the API-only secret file")
	}
	providerConfig.ClientSecret = "loaded-by-api-at-startup"
	if e = providerConfig.Validate(); e != nil {
		return c, e
	}
	if e = c.SMTP.Validate(); e != nil {
		return c, e
	}
	return c, nil
}
func runtimeSecret(directory, name string) (string, error) {
	b, e := os.ReadFile(filepath.Join(directory, name))
	if e != nil {
		return "", errors.New("required auth secret unavailable")
	}
	v := string(b)
	if len(v) < 32 || len(v) > 1024 || strings.TrimSpace(v) != v {
		return "", errors.New("invalid auth secret")
	}
	return v, nil
}
func OpenRuntime(ctx context.Context, c LocalConfig, secrets string, worker bool) (*Store, *Invitations, error) {
	role := "api"
	pool := 5
	if worker {
		role = "worker"
		pool = 3
	}
	password, e := runtimeSecret(secrets, "auth_"+role+"_password")
	if e != nil {
		return nil, nil, e
	}
	db, e := ec2test.Open(ctx, mysqlstore.Config{Address: c.Address, Database: c.Database, User: "local_auth_" + role, Password: password, AllowInsecureTransport: true, MaxOpen: pool, MaxIdle: pool, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: 5 * time.Minute, ConnMaxIdleTime: time.Minute, LockWaitTimeout: 3 * time.Second, InterpolateParameters: true})
	if e != nil {
		return nil, nil, e
	}
	s, e := New(db, c.Database)
	if e != nil {
		db.Close()
		return nil, nil, e
	}
	s.role = role
	if e = s.Ready(ctx); e != nil {
		db.Close()
		return nil, nil, e
	}
	key, e := runtimeSecret(secrets, "auth_invitation_key")
	if e != nil {
		db.Close()
		return nil, nil, e
	}
	inv, e := NewInvitations(s, c.Provider.IssuerURL, c.InvitationOrigin, key)
	if e != nil {
		db.Close()
		return nil, nil, e
	}
	return s, inv, nil
}
func API(ctx context.Context, c LocalConfig, secrets string, s *Store, inv *Invitations, logger *slog.Logger) (http.Handler, error) {
	clientSecret, e := os.ReadFile(filepath.Join(secrets, "auth_oidc_client_secret"))
	if e != nil || len(clientSecret) == 0 || len(clientSecret) > 4096 || strings.TrimSpace(string(clientSecret)) != string(clientSecret) {
		return nil, errors.New("OIDC client credential unavailable")
	}
	c.Provider.ClientSecret = string(clientSecret)
	provider, e := oidcprovider.New(ctx, c.Provider)
	if e != nil {
		return nil, e
	}
	key, e := platform.SigningKey(filepath.Join(secrets, "auth_signing_key"))
	if e != nil {
		return nil, e
	}
	proxy, e := runtimeSecret(secrets, "auth_proxy_token")
	if e != nil {
		return nil, e
	}
	server := platform.Server{Store: s, DefaultTenantID: c.DefaultTenantID, Provider: provider, ProxyToken: proxy, Key: key, KeyID: c.KeyID, Issuer: c.Issuer, Audience: c.Audience, Logger: logger, Invitations: inv, ConfigureMux: ConfigureDocs}
	handler := server.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && origin != c.AppOrigin {
			http.Error(w, "origin denied", http.StatusForbidden)
			return
		}
		handler.ServeHTTP(w, r)
	}), nil
}
