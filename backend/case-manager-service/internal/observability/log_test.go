package observability

import (
	"bytes"
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"testing"
)

func TestLevelsAndSensitiveErrorRedaction(t *testing.T) {
	for _, level := range []string{"debug", "info"} {
		var out bytes.Buffer
		log := New(&out, "test", level)
		log.Debug("debug marker")
		log.Error("database failed", "error", &pgconn.PgError{Code: "23514", Message: "secret findings", Detail: "private account"})
		log.Error("transport failed", "error", errors.New("https://user:password@example.invalid/?token=secret"))
		log.Error("third party failed", "err", "secret raw string")
		got := out.String()
		if strings.Contains(got, "debug marker") != (level == "debug") {
			t.Fatal("log level ignored", got)
		}
		for _, secret := range []string{"secret", "private account", "password", "example.invalid"} {
			if strings.Contains(got, secret) {
				t.Fatal("sensitive error leaked", got)
			}
		}
		if !strings.Contains(got, `"sqlstate":"23514"`) || !strings.Contains(got, `"service":"case-manager-service"`) {
			t.Fatal("missing safe diagnostic fields", got)
		}
		if Logger(WithLogger(context.Background(), log)) != log {
			t.Fatal("context logger lost")
		}
	}
}
