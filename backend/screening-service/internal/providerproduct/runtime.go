package providerproduct

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconfig"
	"github.com/jackc/pgx/v5/pgxpool"
)

func OpenDatabase(ctx context.Context) (*pgxpool.Pool, error) {
	dsn, e := sourceconfig.ReadSecret("PROVIDER_DATABASE_URL")
	if e != nil || dsn == "" {
		return nil, errors.New("PROVIDER_DATABASE_URL or its secret file is required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		return nil, errors.New("invalid provider database configuration")
	}
	max := 8
	if raw := os.Getenv("PROVIDER_DATABASE_MAX_CONNS"); raw != "" {
		max, e = strconv.Atoi(raw)
		if e != nil || max < 1 || max > 64 {
			return nil, errors.New("PROVIDER_DATABASE_MAX_CONNS must be 1..64")
		}
	}
	cfg.MaxConns = int32(max)
	cfg.MinConns = 0
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.ConnConfig.RuntimeParams["application_name"] = "screening-provider"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "2000"
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return nil, errors.New("provider database unavailable")
	}
	if e = db.Ping(ctx); e != nil {
		db.Close()
		return nil, errors.New("provider database unavailable")
	}
	return db, nil
}

func ListenAddress() (string, error) {
	port := os.Getenv("PROVIDER_PORT")
	if port == "" {
		port = "8091"
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return "", errors.New("invalid provider port")
	}
	host := os.Getenv("PROVIDER_HOST")
	return net.JoinHostPort(host, port), nil
}
