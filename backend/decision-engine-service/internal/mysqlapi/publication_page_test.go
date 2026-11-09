package mysqlapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

func TestPublicationCursorStrictDecoding(t *testing.T) {
	c := mysql.PublicationPageCursor{Version: 1, TenantID: uuid.New(), ScenarioID: uuid.New(), ID: uuid.New(), CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	raw, _ := json.Marshal(c)
	encode := func(raw string) string { return base64.RawURLEncoding.EncodeToString([]byte(raw)) }
	f, err := publicationPageFilter(url.Values{"limit": {"2"}, "cursor": {encode(string(raw))}})
	if err != nil || f.Limit != 2 || f.After == nil || f.After.ID != c.ID {
		t.Fatal(f, err)
	}
	for _, q := range []url.Values{
		{"unknown": {"x"}}, {"limit": {"1", "2"}}, {"limit": {"-1"}}, {"limit": {"501"}}, {"cursor": {""}}, {"cursor": {"%%%"}},
		{"cursor": {strings.Repeat("a", 2049)}},
		{"cursor": {encode(strings.TrimSuffix(string(raw), "}") + `,"v":1}`)}},
		{"cursor": {encode(strings.TrimSuffix(string(raw), "}") + `,"unknown":1}`)}},
		{"cursor": {encode(string(raw) + ` {}`)}}, {"cursor": {encode(" " + string(raw))}},
		{"cursor": {encode(strings.Replace(string(raw), `"v":1`, `"V":1`, 1))}},
	} {
		if _, err := publicationPageFilter(q); !errors.Is(err, mysql.ErrExecutionPage) {
			t.Fatal("accepted malformed cursor/query", q, err)
		}
	}
}
