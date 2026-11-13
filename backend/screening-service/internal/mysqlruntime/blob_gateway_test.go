package mysqlruntime

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
)

func TestEC2EvidenceUsesGatewayServicePrefix(t *testing.T) {
	const origin = "http://192.0.2.10:8088"
	for _, gateway := range []bool{false, true} {
		profile := ec2test.Config{Address: "writer.cluster-test.us-east-1.rds.amazonaws.com:3306", AdminUser: "admin", CAFile: "unused.pem", AccountHost: "10.0.1.10/255.255.255.255", Origins: []string{origin}, Gateway: gateway}
		raw, err := json.Marshal(profile)
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), "ec2.json")
		if err := os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(ec2test.Environment, file)
		blob, err := NewLocalBlob(&sql.DB{}, "fraud", origin)
		if err != nil {
			t.Fatal(err)
		}
		want := origin
		if gateway {
			want += "/services/screening"
		}
		if blob.origin != want {
			t.Fatal(blob.origin, want)
		}
		if _, err := NewLocalBlob(&sql.DB{}, "fraud", "http://192.0.2.11:8088"); err == nil {
			t.Fatal("unlisted public origin accepted")
		}
	}
}
