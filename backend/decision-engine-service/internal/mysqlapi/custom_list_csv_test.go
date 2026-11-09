package mysqlapi

import (
	"bytes"
	"errors"
	"mime/multipart"
	"reflect"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

func TestCustomListCSVContractAndBounds(t *testing.T) {
	upload := func(name, csv string, extra bool) (string, string) {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		p, e := w.CreateFormFile(name, "list.csv")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = p.Write([]byte(csv)); e != nil {
			t.Fatal(e)
		}
		if extra {
			if e = w.WriteField("extra", "forged"); e != nil {
				t.Fatal(e)
			}
		}
		if e = w.Close(); e != nil {
			t.Fatal(e)
		}
		return b.String(), w.FormDataContentType()
	}
	for _, tc := range []struct {
		name, csv string
		rows      int
		extra     bool
		want      []string
		err       error
	}{
		{"header", "\uFEFFvalues,ignored\n A ,x\n\"B,comma\",x\n", 2, false, []string{"A", "B,comma"}, nil},
		{"no header", "a\nb\n", 2, false, []string{"a", "b"}, nil},
		{"empty", "", 2, false, []string{}, nil},
		{"bad quote", "\"unterminated", 2, false, nil, errExecutionHTTPBody},
		{"extra part", "a\n", 2, true, nil, errExecutionHTTPBody},
		{"row bound", "values\na\nb\nc\n", 2, false, nil, mysql.ErrProjectionLimit},
		{"duplicate bound", "a\na\na\na\n", 2, false, nil, mysql.ErrProjectionLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, kind := upload("file", tc.csv, tc.extra)
			got, e := customListCSV(strings.NewReader(raw), kind, 4096, tc.rows)
			if !errors.Is(e, tc.err) || !reflect.DeepEqual(got, tc.want) {
				t.Fatal(got, e)
			}
			if _, e := customListCSV(strings.NewReader(raw), kind, int64(len(raw)-1), tc.rows); !errors.Is(e, errExecutionHTTPSize) {
				t.Fatal("body cap", e)
			}
		})
	}
	raw, kind := upload("wrong", "a\n", false)
	if _, e := customListCSV(strings.NewReader(raw), kind, 4096, 2); !errors.Is(e, errExecutionHTTPBody) {
		t.Fatal(e)
	}
	if _, e := customListCSV(strings.NewReader(raw), "application/json", 4096, 2); !errors.Is(e, errExecutionHTTPBody) {
		t.Fatal(e)
	}
	raw, kind = upload("file", "a\n", false)
	if _, e := customListCSV(strings.NewReader(raw[:len(raw)-10]), kind, 4096, 2); !errors.Is(e, errExecutionHTTPBody) {
		t.Fatal("truncated multipart accepted", e)
	}
}
