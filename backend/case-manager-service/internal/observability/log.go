package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/jackc/pgx/v5/pgconn"
)

type loggerKey struct{}

func New(out io.Writer, component, configuredLevel string) *slog.Logger {
	level := slog.LevelInfo
	err := level.UnmarshalText([]byte(strings.ToUpper(configuredLevel)))
	if err != nil {
		level = slog.LevelInfo
	}
	l := slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level, ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == "error" || a.Key == "err" {
			if err, ok := a.Value.Any().(error); ok {
				return slog.Group(a.Key, ErrorFields(err)...)
			}
			return slog.String(a.Key, "details omitted; see operation error_class and sqlstate")
		}
		return a
	}})).With("service", "case-manager-service", "component", component)
	if err != nil && configuredLevel != "" {
		l.Warn("invalid LOG_LEVEL; using info", "event", "logging.configuration_invalid")
	}
	return l
}

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

func Logger(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// Do not log raw database/provider errors: their messages can include row data,
// connection strings, credentials or investigator findings.
func ErrorFields(err error) []any {
	if err == nil {
		return nil
	}
	class := "internal"
	switch {
	case errors.Is(err, context.Canceled):
		class = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		class = "timeout"
	case errors.Is(err, casepkg.ErrForbidden):
		class = "forbidden"
	case errors.Is(err, casepkg.ErrConflict):
		class = "conflict"
	case errors.Is(err, casepkg.ErrValidation):
		class = "validation"
	case errors.Is(err, casepkg.ErrNotFound):
		class = "not_found"
	}
	fields := []any{"error_class", class, "error_type", fmt.Sprintf("%T", err)}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		fields = append(fields, "sqlstate", pg.Code)
	}
	return fields
}
