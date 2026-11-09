package mysqlapi

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

func TestHistoryQueryRejectsAmbiguityAndCursorReplay(t *testing.T) {
	tenant, scenario := uuid.NewString(), uuid.NewString()
	for _, value := range []string{string([]byte{0xff}), strings.Repeat("a", 769)} {
		if _, e := parseHistoryQuery(url.Values{"search": {value}}, "list", tenant, "", 10); e == nil {
			t.Fatal("accepted invalid or unpageable filter")
		}
	}
	for _, query := range []string{"limit=0", "limit=-1", "limit=11", "limit=1&limit=2", "offset=1", "min_score=2147483648", "max_score=-2147483649", "min_score=2&max_score=1", "include_total_count=yes", "decision_id=bad", "scenario_id=" + uuid.Nil.String(), "cursor=", "ignored=1"} {
		q, e := url.ParseQuery(query)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = parseHistoryQuery(q, "list", tenant, "", 10); e == nil {
			t.Error("accepted", query)
		}
	}
	valid := historyCursor{1, tenant, ports.DecisionListFilter{ScenarioID: scenario}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), uuid.NewString()}
	encode := func(v historyCursor) string {
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	q := url.Values{"cursor": {encode(valid)}}
	if f, e := parseHistoryQuery(q, "scenario", tenant, scenario, 10); e != nil || f.limit != 10 || f.after == nil {
		t.Fatal(f, e)
	}
	for _, field := range []string{"decision_id", "scenario_id", "object_id", "object_type", "outcome", "object_id_prefix", "search", "min_score", "max_score"} {
		t.Run("cursor rejects changed "+field, func(t *testing.T) {
			cursor := valid
			cursor.Filter = ports.DecisionListFilter{}
			changed := url.Values{"cursor": {encode(cursor)}}
			value := "changed"
			if field == "decision_id" || field == "scenario_id" {
				value = uuid.NewString()
			}
			if field == "min_score" || field == "max_score" {
				value = "0"
			}
			changed.Set(field, value)
			if _, err := parseHistoryQuery(changed, "list", tenant, "", 10); err == nil {
				t.Fatal("cursor accepted changed filter", field)
			}
		})
	}
	for _, change := range []func(*historyCursor){func(c *historyCursor) { c.Version = 2 }, func(c *historyCursor) { c.Tenant = uuid.NewString() }, func(c *historyCursor) { c.Filter.Outcome = "review" }, func(c *historyCursor) { c.CreatedAt = c.CreatedAt.Add(time.Nanosecond) }, func(c *historyCursor) { c.CreatedAt = time.Time{} }, func(c *historyCursor) { c.ID = uuid.Nil.String() }} {
		v := valid
		change(&v)
		q.Set("cursor", encode(v))
		if _, e := parseHistoryQuery(q, "scenario", tenant, scenario, 10); e == nil {
			t.Fatal("accepted invalid cursor", v)
		}
	}
	for _, op := range []string{"detail", "run", "runs", "workflows", "requests", "request"} {
		if _, e := parseHistoryQuery(url.Values{"limit": {"1"}}, op, tenant, scenario, 10); e == nil {
			t.Fatal("unexpected query", op)
		}
	}
	if _, e := parseHistoryQuery(url.Values{"limit": {"1"}}, "count", tenant, "", 10); e == nil {
		t.Fatal("count accepted paging")
	}
	if _, e := parseHistoryQuery(url.Values{"object_id": {"Wallet ", "Wallet"}}, "snoozes", tenant, scenario, 10); e == nil {
		t.Fatal("duplicate snooze selector")
	}
}
