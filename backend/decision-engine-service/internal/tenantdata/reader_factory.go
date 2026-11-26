package tenantdata

import (
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	ingestionclient "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
)

const (
	ReadModeIngestionHTTP = "ingestion_http"
	ReadModeDirectDB      = "direct_db"
)

type ReaderOptions struct {
	StatementTimeout       time.Duration
	EnableRollupAggregates bool
	RollupShadowMode       bool
}

func NewReader(readMode string, db *pgxpool.Pool, dataModelReader ports.DataModelReader, ingestionServiceURL string, timeout time.Duration, statementTimeout ...time.Duration) ports.TenantDataReader {
	var opts ReaderOptions
	if len(statementTimeout) > 0 {
		opts.StatementTimeout = statementTimeout[0]
	}
	return NewReaderWithOptions(readMode, db, dataModelReader, ingestionServiceURL, timeout, opts)
}

func NewReaderWithOptions(readMode string, db *pgxpool.Pool, dataModelReader ports.DataModelReader, ingestionServiceURL string, timeout time.Duration, opts ReaderOptions) ports.TenantDataReader {
	switch strings.ToLower(strings.TrimSpace(readMode)) {
	case ReadModeDirectDB:
		if db != nil {
			reader := storepostgres.NewTenantDataReader(db, dataModelReader)
			if opts.StatementTimeout > 0 {
				reader = reader.WithStatementTimeout(opts.StatementTimeout)
			}
			if opts.EnableRollupAggregates || opts.RollupShadowMode {
				cache := storepostgres.NewDirectDBRollupCache(db, 5*time.Second)
				reader = reader.WithRollups(cache, opts.EnableRollupAggregates, opts.RollupShadowMode)
			}
			return reader
		}
	}
	return ingestionclient.NewHTTPClient(ingestionServiceURL, timeout)
}
