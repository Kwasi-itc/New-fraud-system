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

func NewReader(readMode string, db *pgxpool.Pool, dataModelReader ports.DataModelReader, ingestionServiceURL string, timeout time.Duration, statementTimeout ...time.Duration) ports.TenantDataReader {
	switch strings.ToLower(strings.TrimSpace(readMode)) {
	case ReadModeDirectDB:
		if db != nil {
			reader := storepostgres.NewTenantDataReader(db, dataModelReader)
			if len(statementTimeout) > 0 && statementTimeout[0] > 0 {
				reader = reader.WithStatementTimeout(statementTimeout[0])
			}
			return reader
		}
	}
	return ingestionclient.NewHTTPClient(ingestionServiceURL, timeout)
}
