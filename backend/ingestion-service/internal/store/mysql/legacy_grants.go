package mysql

import (
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"regexp"
)

// LegacyOperatorGrants is a distinct offline principal, never an API/worker
// grant extension. Preview principals receive SELECT only. Import cannot mutate
// physical rows, successful-ingestion evidence, queues or owner metadata.
func LegacyOperatorGrants(database, account string, preview bool) ([]string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`).MatchString(account) || account == "root" {
		return nil, errors.New("dedicated legacy operator required")
	}
	to := " TO '" + account + "'@'%'"
	var grants []string
	for _, table := range []string{"data_model_tenants", "data_model_tables", "data_model_ingestion_contracts", "ingestion_request_receipts", "ingestion_legacy_imports"} {
		grants = append(grants, "GRANT SELECT ON "+s+"."+table+to)
	}
	if !preview {
		grants = append(grants, "GRANT INSERT,SELECT(key_digest),UPDATE(key_digest) ON "+s+".ingestion_receipt_buckets"+to, "GRANT INSERT ON "+s+".ingestion_request_receipts"+to, "GRANT INSERT ON "+s+".ingestion_legacy_imports"+to)
	}
	return grants, nil
}
