package mysqlapi

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

func TestExecutionPageStrictQueryAndCursor(t *testing.T) {
	for _, query := range []string{"limit=0", "limit=501", "limit=1&limit=2", "min_attempt_count=-1", "min_attempt_count=2147483648", "status=unknown", "status=", "offset=100", "cursor=", "cursor=not-json", "limit=1&unknown=true"} {
		q, err := url.ParseQuery(query)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := executionPageFilter(q); err == nil {
			t.Fatal("accepted", query)
		}
	}
	c := mysql.ExecutionPageCursor{TenantID: uuid.New(), ScenarioID: uuid.New(), CreatedAt: time.Now().UTC().Truncate(time.Microsecond), ID: uuid.New()}
	raw, _ := json.Marshal(c)
	f, err := executionPageFilter(url.Values{"cursor": {base64.RawURLEncoding.EncodeToString(raw)}, "limit": {"2"}, "min_attempt_count": {"3"}, "status": {"cancelled"}})
	if err != nil || f.Limit != 2 || f.MinAttemptCount != 3 || f.Status != "cancelled" || f.After == nil || *f.After != c {
		t.Fatal(f, err)
	}
	for _, bad := range [][]byte{append(raw, []byte(` {}`)...), []byte(`{"id":"` + uuid.NewString() + `","id":"` + uuid.NewString() + `"}`), []byte(`{"forged":true}`)} {
		if _, err := executionPageFilter(url.Values{"cursor": {base64.RawURLEncoding.EncodeToString(bad)}}); err == nil {
			t.Fatal("noncanonical cursor", string(bad))
		}
	}
}
