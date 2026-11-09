package mysqlruntime

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/google/uuid"
)

func testCustomListAPIs(t *testing.T, h http.Handler, base string, db *sql.DB, schema string, revoked *atomic.Bool) {
	request := func(t *testing.T, method, path, body string, auth bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			req.Header.Set("Authorization", "Bearer fixture-only")
		}
		if strings.HasPrefix(body, "--fixture-lists") {
			req.Header.Set("Content-Type", "multipart/form-data; boundary=fixture-lists")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
		return rec
	}
	id := func(t *testing.T, rec *httptest.ResponseRecorder, key string) string {
		t.Helper()
		var out map[string]struct {
			ID string `json:"id"`
		}
		if e := json.Unmarshal(rec.Body.Bytes(), &out); e != nil || out[key].ID == "" {
			t.Fatal(rec.Body.String(), e)
		}
		return out[key].ID
	}
	exec := func(q string) {
		t.Helper()
		if _, e := db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	security := func(t *testing.T, method, path, body string) {
		t.Helper()
		request(t, method, path, body, false, 401)
		request(t, method, strings.Replace(path, base, "/v1/tenants/"+uuid.NewString(), 1), body, true, 403)
		revoked.Store(true)
		defer revoked.Store(false)
		request(t, method, path, body, true, 403)
	}
	upload := func(csv string) string {
		t.Helper()
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		if e := w.SetBoundary("fixture-lists"); e != nil {
			t.Fatal(e)
		}
		p, e := w.CreateFormFile("file", "values.csv")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = p.Write([]byte(csv)); e != nil {
			t.Fatal(e)
		}
		if e = w.Close(); e != nil {
			t.Fatal(e)
		}
		return b.String()
	}
	path := base + "/platform/custom-lists"
	body := `{"name":"Risk list","description":"Exact values","kind":"generic_text"}`
	var list, entry string
	t.Run("T33 custom list create", func(t *testing.T) {
		security(t, "POST", path, body)
		request(t, "POST", path, strings.TrimSuffix(body, "}")+`,"tenant_id":"forged"}`, true, 400)
		request(t, "POST", path, strings.Replace(body, "Risk list", `Bad\u0000name`, 1), true, 422)
		list = id(t, request(t, "POST", path, body, true, 201), "custom_list")
		request(t, "POST", path, strings.Replace(body, "Risk list", "risk list", 1), true, 409)
	})
	if list == "" {
		t.Fatal("list prerequisite")
	}
	p := path + "/" + list
	ep := p + "/entries"
	t.Run("T31 custom list list", func(t *testing.T) {
		security(t, "GET", path, "")
		rec := request(t, "GET", path, "", true, 200)
		var out struct {
			Lists []dto.CustomListResponse `json:"custom_lists"`
		}
		if e := json.Unmarshal(rec.Body.Bytes(), &out); e != nil || len(out.Lists) != 1 || out.Lists[0].ID != list {
			t.Fatal(rec.Body.String(), e)
		}
		request(t, "GET", path+"?ignored=1", "", true, 400)
	})
	t.Run("T32 custom list detail", func(t *testing.T) {
		security(t, "GET", p, "")
		if got := id(t, request(t, "GET", p, "", true, 200), "custom_list"); got != list {
			t.Fatal(got)
		}
		request(t, "GET", path+"/"+uuid.NewString(), "", true, 404)
	})
	t.Run("T37 custom list entry create", func(t *testing.T) {
		b := `{"value":"Case Preserved "}`
		security(t, "POST", ep, b)
		request(t, "POST", ep, `{"value":null}`, true, 400)
		request(t, "POST", ep, `{"value":"bad\u0000text"}`, true, 422)
		entry = id(t, request(t, "POST", ep, b, true, 201), "custom_list_entry")
		request(t, "POST", path+"/"+uuid.NewString()+"/entries", b, true, 404)
	})
	if entry == "" {
		t.Fatal("entry prerequisite")
	}
	entries := func(t *testing.T) []dto.CustomListEntryResponse {
		t.Helper()
		rec := request(t, "GET", ep, "", true, 200)
		var out struct {
			Entries []dto.CustomListEntryResponse `json:"custom_list_entries"`
		}
		if e := json.Unmarshal(rec.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		return out.Entries
	}
	t.Run("T36 custom list entry list", func(t *testing.T) {
		security(t, "GET", ep, "")
		v := entries(t)
		if len(v) != 1 || v[0].ID != entry || v[0].Value != "Case Preserved " || v[0].ListName != "Risk list" {
			t.Fatal(v)
		}
		request(t, "GET", path+"/"+uuid.NewString()+"/entries", "", true, 404)
	})
	t.Run("T34 custom list update", func(t *testing.T) {
		renamed := strings.Replace(body, "Risk list", "Renamed list", 1)
		security(t, "PUT", p, renamed)
		before := request(t, "GET", p, "", true, 200).Body.String()
		exec("CREATE TRIGGER " + schema + ".fixture_list_rename BEFORE UPDATE ON " + schema + ".decision_engine_custom_list_entries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='private rename fault'")
		rec := request(t, "PUT", p, renamed, true, 500)
		if strings.Contains(rec.Body.String(), "private rename") {
			t.Fatal("SQL exposed")
		}
		exec("DROP TRIGGER " + schema + ".fixture_list_rename")
		if after := request(t, "GET", p, "", true, 200).Body.String(); after != before {
			t.Fatal("parent rename did not roll back")
		}
		if v := entries(t); v[0].ListName != "Risk list" {
			t.Fatal(v)
		}
		request(t, "PUT", p, renamed, true, 200)
		if v := entries(t); v[0].ListName != "Renamed list" {
			t.Fatal(v)
		}
	})
	other := id(t, request(t, "POST", path, strings.Replace(body, "Risk list", "Other list", 1), true, 201), "custom_list")
	t.Run("T39 custom list entry update", func(t *testing.T) {
		target := ep + "/" + entry
		b := `{"value":"Updated "}`
		security(t, "PUT", target, b)
		before := entries(t)[0]
		request(t, "PUT", path+"/"+other+"/entries/"+entry, b, true, 404)
		request(t, "PUT", target, b, true, 200)
		after := entries(t)[0]
		if after.Value != "Updated " || after.ID != before.ID || !after.CreatedAt.Equal(before.CreatedAt) || after.ListName != "Renamed list" {
			t.Fatal(before, after)
		}
	})
	t.Run("T38 custom list CSV import", func(t *testing.T) {
		target := ep + "/import"
		b := upload("\uFEFFvalues,ignored\n A ,x\nA,y\n\"B,comma\",z\nCafé ,z\n")
		security(t, "POST", target, b)
		rec := request(t, "POST", target, b, true, 200)
		if rec.Body.String() != `{"imported_count":3}` {
			t.Fatal(rec.Body.String())
		}
		before := len(entries(t))
		request(t, "POST", target, upload("values\n\"unterminated"), true, 400)
		if len(entries(t)) != before {
			t.Fatal("invalid CSV mutated")
		}
		var csv strings.Builder
		csv.WriteString("values\n")
		for i := 0; i < 120; i++ {
			fmt.Fprintf(&csv, "bulk-%03d\n", i)
		}
		exec("CREATE TRIGGER " + schema + ".fixture_list_import BEFORE INSERT ON " + schema + ".decision_engine_custom_list_entries FOR EACH ROW BEGIN IF NEW.value='bulk-105' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='import fault'; END IF; END")
		request(t, "POST", target, upload(csv.String()), true, 500)
		exec("DROP TRIGGER " + schema + ".fixture_list_import")
		if len(entries(t)) != before {
			t.Fatal("earlier import batch was not rolled back")
		}
		request(t, "POST", target, upload(csv.String()), true, 200)
		if len(entries(t)) != before+120 {
			t.Fatal("bulk import incomplete")
		}
		request(t, "POST", target, upload(strings.Repeat("row\n", 502)), true, 413)
		request(t, "POST", target, upload(strings.Repeat("x", (1<<20)+1)), true, 413)
	})
	t.Run("T40 custom list entry delete", func(t *testing.T) {
		target := ep + "/" + entry
		security(t, "DELETE", target, "")
		request(t, "DELETE", path+"/"+other+"/entries/"+entry, "", true, 404)
		request(t, "DELETE", target, "", true, 204)
		request(t, "DELETE", target, "", true, 404)
		for _, v := range entries(t) {
			if v.ID == entry {
				t.Fatal("entry not removed")
			}
		}
	})
	t.Run("T35 custom list delete", func(t *testing.T) {
		security(t, "DELETE", p, "")
		request(t, "DELETE", p, `{"actor":"forged"}`, true, 400)
		request(t, "DELETE", p, "", true, 204)
		request(t, "GET", p, "", true, 404)
		request(t, "GET", ep, "", true, 404)
		var count int
		if e := db.QueryRow("SELECT COUNT(*) FROM "+schema+".decision_engine_custom_list_entries WHERE list_id=UUID_TO_BIN(?)", list).Scan(&count); e != nil || count != 0 {
			t.Fatal("entries not cascaded", count, e)
		}
		request(t, "GET", path+"/"+other, "", true, 200)
	})
}
