package mysqlapi

import (
	"net/url"
	"testing"
)

func TestPlatformQueryExactContract(t *testing.T) {
	for _, tc := range []struct {
		op, query string
		valid     bool
	}{
		{"tags", "object_type=&object_id=", true}, {"tags", "object_type=wallets&object_id=+", true},
		{"tags", "object_type=x", false}, {"tags", "object_type=x&object_id=a&object_id=b", false},
		{"tags", "object_type=x&object_id=a&limit=1", false}, {"entries", "list_name=", true},
		{"entries", "", false}, {"flags", "ip_address=2001%3Adb8%3A%3A1+", true},
		{"flags", "ip_address=%00", false}, {"flags", "ip_address=%FF", false},
		{"flag.create", "", true}, {"flag.create", "ip_address=x", false},
	} {
		t.Run(tc.op+"/"+tc.query, func(t *testing.T) {
			q, e := url.ParseQuery(tc.query)
			if e != nil {
				t.Fatal(e)
			}
			if e = platformQuery(q, tc.op); (e == nil) != tc.valid {
				t.Fatal(e)
			}
		})
	}
}
