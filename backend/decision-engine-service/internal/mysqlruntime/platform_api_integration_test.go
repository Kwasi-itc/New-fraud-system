package mysqlruntime

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func testPlatformAPIs(t *testing.T, h http.Handler, base string, db *sql.DB, schema string, revoked, denied, missing *atomic.Bool) {
	request := func(t *testing.T, method, path, body string, auth bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			req.Header.Set("Authorization", "Bearer fixture-only")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s got %d want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
		return rec
	}
	security := func(t *testing.T, method, path, body string) {
		t.Helper()
		request(t, method, path, body, false, 401)
		request(t, method, strings.Replace(path, base, "/v1/tenants/"+uuid.NewString(), 1), body, true, 403)
		revoked.Store(true)
		defer revoked.Store(false)
		request(t, method, path, body, true, 403)
	}
	array := func(t *testing.T, r *httptest.ResponseRecorder, key string, n int) []map[string]any {
		t.Helper()
		var v map[string][]map[string]any
		if e := json.Unmarshal(r.Body.Bytes(), &v); e != nil || v[key] == nil || len(v[key]) != n {
			t.Fatalf("array: %s %v", r.Body.String(), e)
		}
		return v[key]
	}
	single := func(t *testing.T, r *httptest.ResponseRecorder, key string) map[string]any {
		t.Helper()
		var v map[string]map[string]any
		if e := json.Unmarshal(r.Body.Bytes(), &v); e != nil || v[key]["id"] == nil || v[key]["tenant_id"] != strings.TrimPrefix(base, "/v1/tenants/") {
			t.Fatalf("single: %s %v", r.Body.String(), e)
		}
		return v[key]
	}
	p := base + "/platform/"
	tag := `{"object_type":"wallets","object_id":"platform-object ","tag":"Watch "}`
	risk := `{"object_type":"wallets","object_id":"platform-object ","risk_level":"high"}`
	flag := `{"ip_address":"2001:db8::1 ","flag":"VPN "}`
	t.Run("T41 legacy list name lookup", func(t *testing.T) {
		v := single(t, request(t, "POST", p+"custom-lists", `{"name":"Café Platform ","description":"fixture","kind":"generic_text"}`, true, 201), "custom_list")
		request(t, "POST", p+"custom-lists/"+v["id"].(string)+"/entries", `{"value":"Keep "}`, true, 201)
		// Source ListByName uses exact equality; case-folded uniqueness does
		// not make entry lookup case insensitive.
		path := p + "custom-list-entries?list_name=Caf%C3%A9+Platform+"
		security(t, "GET", path, "")
		rows := array(t, request(t, "GET", path, "", true, 200), "custom_list_entries", 1)
		if rows[0]["value"] != "Keep " {
			t.Fatal(rows)
		}
		array(t, request(t, "GET", p+"custom-list-entries?list_name=Cafe+Platform+", "", true, 200), "custom_list_entries", 0)
		array(t, request(t, "GET", p+"custom-list-entries?list_name=CAF%C3%89+PLATFORM+", "", true, 200), "custom_list_entries", 0)
		array(t, request(t, "GET", p+"custom-list-entries?list_name=Caf%C3%A9+Platform", "", true, 200), "custom_list_entries", 0)
	})
	t.Run("T43 record tag creation", func(t *testing.T) {
		security(t, "POST", p+"record-tags", tag)
		v := single(t, request(t, "POST", p+"record-tags", tag, true, 201), "record_tag")
		if v["object_id"] != "platform-object " || v["tag"] != "Watch " {
			t.Fatal(v)
		}
	})
	t.Run("T42 record tag lookup", func(t *testing.T) {
		path := p + "record-tags?object_type=wallets&object_id=platform-object+"
		security(t, "GET", path, "")
		array(t, request(t, "GET", path, "", true, 200), "record_tags", 1)
		array(t, request(t, "GET", strings.TrimSuffix(path, "+"), "", true, 200), "record_tags", 0)
	})
	t.Run("T44 risk snapshot creation", func(t *testing.T) {
		security(t, "POST", p+"risk-snapshots", risk)
		v := single(t, request(t, "POST", p+"risk-snapshots", risk, true, 201), "risk_snapshot")
		if v["risk_level"] != "high" || v["object_id"] != "platform-object " {
			t.Fatal(v)
		}
	})
	t.Run("T46 IP flag creation", func(t *testing.T) {
		security(t, "POST", p+"ip-flags", flag)
		v := single(t, request(t, "POST", p+"ip-flags", flag, true, 201), "ip_flag")
		if v["ip_address"] != "2001:db8::1 " || v["flag"] != "VPN " {
			t.Fatal(v)
		}
	})
	t.Run("T45 IP flag lookup", func(t *testing.T) {
		path := p + "ip-flags?ip_address=2001%3Adb8%3A%3A1+"
		security(t, "GET", path, "")
		array(t, request(t, "GET", path, "", true, 200), "ip_flags", 1)
		array(t, request(t, "GET", strings.TrimSuffix(path, "+"), "", true, 200), "ip_flags", 0)
	})
	t.Run("T47 separate mutation authority", func(t *testing.T) {
		defer denied.Store(false)
		defer missing.Store(false)
		for _, x := range []struct{ path, body string }{{"record-tags", tag}, {"risk-snapshots", risk}, {"ip-flags", flag}} {
			denied.Store(true)
			request(t, "POST", p+x.path, x.body, true, 403)
			denied.Store(false)
			missing.Store(true)
			request(t, "POST", p+x.path, x.body, true, 503)
			missing.Store(false)
		}
		missing.Store(true)
		array(t, request(t, "GET", p+"record-tags?object_type=wallets&object_id=platform-object+", "", true, 200), "record_tags", 1)
	})
	t.Run("T48 strict requests and exact empty identity", func(t *testing.T) {
		for _, path := range []string{"record-tags", "record-tags?object_type=wallets", "record-tags?object_type=x&object_id=a&object_id=b", "record-tags?object_type=x&object_id=a&extra=1", "ip-flags?ip_address=%00", "ip-flags?ip_address=%FF", "ip-flags?ip_address=%ZZ", "custom-list-entries?list_name=x&list_name=y"} {
			request(t, "GET", p+path, "", true, 400)
		}
		for _, body := range []string{`{}`, `{"object_type":"x","object_id":null,"tag":"x"}`, `{"object_type":"x","object_id":"a","tag":"x","tag":"y"}`, `{"object_type":"x","object_id":"a","tag":"x","tenant_id":"forged"}`} {
			request(t, "POST", p+"record-tags", body, true, 400)
		}
		request(t, "POST", p+"record-tags", strings.Replace(tag, "Watch ", `Bad\u0000`, 1), true, 422)
		request(t, "POST", p+"record-tags?extra=1", tag, true, 400)
		request(t, "POST", p+"record-tags", `{"object_type":"","object_id":"","tag":""}`, true, 201)
		array(t, request(t, "GET", p+"record-tags?object_type=&object_id=", "", true, 200), "record_tags", 1)
		request(t, "POST", p+"ip-flags", `{"ip_address":"","flag":""}`, true, 201)
		array(t, request(t, "GET", p+"ip-flags?ip_address=", "", true, 200), "ip_flags", 1)
	})
	t.Run("T49 bounded platform projections", func(t *testing.T) {
		request(t, "POST", p+"record-tags", strings.Replace(tag, "Watch ", strings.Repeat("x", 1<<20), 1), true, 413)
		tenant := mysqlstore.UUID(uuid.MustParse(strings.TrimPrefix(base, "/v1/tenants/")))
		marks := make([]string, 501)
		args := make([]any, 0, 1002)
		for i := range marks {
			marks[i] = "(?,?,'overflow','flag',UTC_TIMESTAMP(6))"
			args = append(args, mysqlstore.UUID(uuid.New()), tenant)
		}
		if _, e := db.Exec("INSERT INTO "+schema+".decision_engine_ip_flags(id,tenant_id,ip_address,flag,created_at) VALUES "+strings.Join(marks, ","), args...); e != nil {
			t.Fatal(e)
		}
		r := request(t, "GET", p+"ip-flags?ip_address=overflow", "", true, 413)
		if strings.Contains(r.Body.String(), "ip_flags") {
			t.Fatal("partial success", r.Body.String())
		}
		array(t, request(t, "GET", p+"ip-flags?ip_address=2001%3Adb8%3A%3A1+", "", true, 200), "ip_flags", 1)
	})
	t.Run("T50 atomic failure and evaluator records", func(t *testing.T) {
		// Force a successful insert to fail during readback by widening its text
		// beyond the projection budget. The surrounding mutation must roll it back.
		trigger := schema + ".platform_api_fault"
		if _, e := db.Exec("CREATE TRIGGER " + trigger + " BEFORE INSERT ON " + schema + ".decision_engine_record_tags FOR EACH ROW SET NEW.tag=IF(NEW.tag='rollback',REPEAT('x',2100000),NEW.tag)"); e != nil {
			t.Fatal(e)
		}
		defer func() {
			if _, e := db.Exec("DROP TRIGGER " + trigger); e != nil {
				t.Error(e)
			}
		}()
		request(t, "POST", p+"record-tags", `{"object_type":"wallets","object_id":"rollback","tag":"rollback"}`, true, 413)
		array(t, request(t, "GET", p+"record-tags?object_type=wallets&object_id=rollback", "", true, 200), "record_tags", 0)
		for _, table := range []string{"record_tags", "risk_snapshots", "ip_flags"} {
			var n int
			field, value := "object_id", "platform-object "
			if table == "ip_flags" {
				field, value = "ip_address", "2001:db8::1 "
			}
			q := fmt.Sprintf("SELECT COUNT(*) FROM %s.decision_engine_%s WHERE tenant_id=? AND CAST(%s AS BINARY)=CAST(? AS BINARY)", schema, table, field)
			if e := db.QueryRow(q, mysqlstore.UUID(uuid.MustParse(strings.TrimPrefix(base, "/v1/tenants/"))), value).Scan(&n); e != nil || n != 1 {
				t.Fatal(table, n, e)
			}
		}
	})
}
