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

func NewReader(readMode string, db *pgxpool.Pool, dataModelReader ports.DataModelReader, ingestionServiceURL string, timeout time.Duration) ports.TenantDataReader {
	switch strings.ToLower(strings.TrimSpace(readMode)) {
	case ReadModeDirectDB:
		if db != nil {
			return storepostgres.NewTenantDataReader(db, dataModelReader)
		}
	}
	return ingestionclient.NewHTTPClient(ingestionServiceURL, timeout)
}
