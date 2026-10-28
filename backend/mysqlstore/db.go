// Package mysqlstore contains MySQL mechanics shared by owner repositories.
// Business authorization, SQL and transaction retry policy belong to callers.
package mysqlstore

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/go-sql-driver/mysql"
)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Identifier quotes a physical registry/configuration identifier. It does not
// authorize access: callers must resolve tenant-owned metadata first.
func Identifier(name string) (string, error) {
	if !identifier.MatchString(name) {
		return "", errors.New("invalid physical database identifier")
	}
	return "`" + name + "`", nil
}

type Config struct {
	Address, Database, User, Password string
	TLS                               *tls.Config
	// AllowInsecureTransport is only for explicitly isolated local test networks.
	AllowInsecureTransport                    bool
	MaxOpen, MaxIdle                          int
	ConnectTimeout, ReadTimeout, WriteTimeout time.Duration
	ConnMaxLifetime, ConnMaxIdleTime          time.Duration
}

func (c Config) connector() (*mysql.Config, error) {
	if _, err := Identifier(c.Database); err != nil {
		return nil, err
	}
	if c.Address == "" || c.User == "" {
		return nil, errors.New("database address and user are required")
	}
	if c.MaxOpen <= 0 || c.MaxIdle < 0 || c.MaxIdle > c.MaxOpen {
		return nil, errors.New("invalid connection pool budget")
	}
	if c.ConnectTimeout <= 0 || c.ReadTimeout <= 0 || c.WriteTimeout <= 0 || c.ConnMaxLifetime <= 0 || c.ConnMaxIdleTime <= 0 {
		return nil, errors.New("positive connection timeouts and lifetimes are required")
	}
	if c.TLS == nil && !c.AllowInsecureTransport {
		return nil, errors.New("verified TLS is required")
	}
	if c.TLS != nil && c.TLS.InsecureSkipVerify {
		return nil, errors.New("TLS verification cannot be disabled")
	}
	cfg := mysql.NewConfig()
	cfg.Net, cfg.Addr, cfg.DBName = "tcp", c.Address, c.Database
	cfg.User, cfg.Passwd = c.User, c.Password
	cfg.ParseTime, cfg.Loc = true, time.UTC
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = c.ConnectTimeout, c.ReadTimeout, c.WriteTimeout
	if c.TLS != nil {
		cfg.TLS = c.TLS.Clone()
		if cfg.TLS.MinVersion < tls.VersionTLS12 {
			cfg.TLS.MinVersion = tls.VersionTLS12
		}
	}
	// No arbitrary DSN flags: multi-statements, interpolation, local infile and
	// matched-row reporting stay disabled. Every new physical connection gets
	// the same UTC/strict/isolation settings, including after pool replacement.
	cfg.MultiStatements, cfg.InterpolateParams, cfg.ClientFoundRows, cfg.AllowAllFiles = false, false, false, false
	cfg.Params = map[string]string{
		"autocommit":            "1",
		"completion_type":       "0",
		"foreign_key_checks":    "1",
		"unique_checks":         "1",
		"time_zone":             "'+00:00'",
		"transaction_isolation": "'READ-COMMITTED'",
		"sql_mode":              "'STRICT_TRANS_TABLES,ERROR_FOR_DIVISION_BY_ZERO,NO_ZERO_DATE,NO_ZERO_IN_DATE,NO_ENGINE_SUBSTITUTION,ONLY_FULL_GROUP_BY'",
	}
	return cfg, nil
}

// Open validates and pings the configured database; it never creates a schema.
func Open(ctx context.Context, c Config) (*sql.DB, error) {
	cfg, err := c.connector()
	if err != nil {
		return nil, err
	}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, fmt.Errorf("create MySQL connector: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(c.MaxOpen)
	db.SetMaxIdleConns(c.MaxIdle)
	db.SetConnMaxLifetime(c.ConnMaxLifetime)
	db.SetConnMaxIdleTime(c.ConnMaxIdleTime)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping MySQL: %w", err)
	}
	return db, nil
}
