package mysqlruntime

import (
	"strings"
	"testing"
)

func TestLocalConsumerGrantsKeepRuntimeAndOwnerBoundaries(t *testing.T) {
	statement, err := localConsumerGrant("tenant_sample_records", "local_ingestion_worker", "172.24.0.0/255.255.0.0")
	if err != nil || !strings.HasPrefix(statement, "GRANT SELECT,INSERT,UPDATE ON `fraud`.`tenant_sample_records` TO ") {
		t.Fatal(statement, err)
	}
	for _, v := range []struct{ table, user, host string }{
		{"tenant_sample_records", "root", "localhost"},
		{"tenant_sample_records", "local_ingestion_worker", "%' IDENTIFIED BY 'unsafe"},
		{"*", "local_ingestion_worker", "%"},
		{"records`; DROP TABLE records", "local_ingestion_worker", "%"},
	} {
		if _, err := localConsumerGrant(v.table, v.user, v.host); err == nil {
			t.Fatal("unsafe grant accepted", v)
		}
	}
	read, err := localConsumerGrant("tenant_sample_records", "local_decision_api", "172.24.0.0/255.255.0.0")
	if err != nil || strings.Contains(read, "INSERT") || strings.Contains(read, "GRANT OPTION") {
		t.Fatal(read, err)
	}
}
