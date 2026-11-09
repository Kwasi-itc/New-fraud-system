package mysqlapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

func publicationPageFilter(q url.Values) (mysql.PublicationPageFilter, error) {
	f := mysql.PublicationPageFilter{Limit: 100}
	for key, values := range q {
		if len(values) != 1 {
			return f, mysql.ErrExecutionPage
		}
		switch key {
		case "limit":
			n, err := strconv.ParseInt(values[0], 10, 32)
			if err != nil || n < 1 || n > 500 {
				return f, mysql.ErrExecutionPage
			}
			f.Limit = int(n)
		case "cursor":
			if len(values[0]) == 0 || len(values[0]) > 2048 {
				return f, mysql.ErrExecutionPage
			}
			raw, err := base64.RawURLEncoding.Strict().DecodeString(values[0])
			if err != nil {
				return f, mysql.ErrExecutionPage
			}
			var c mysql.PublicationPageCursor
			if err := decodeExecutionHTTP(bytes.NewReader(raw), 2048, &c); err != nil {
				return f, mysql.ErrExecutionPage
			}
			canonical, err := json.Marshal(c)
			if err != nil || !bytes.Equal(raw, canonical) {
				return f, mysql.ErrExecutionPage
			}
			f.After = &c
		default:
			return f, mysql.ErrExecutionPage
		}
	}
	return f, nil
}
