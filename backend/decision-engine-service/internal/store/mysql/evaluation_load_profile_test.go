package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// Diagnostic helpers are fixture-only. Retain durations and normalized SQL,
// never object payloads, credentials or provider response bodies.
type evaluationStageTotal struct {
	Calls       int64 `json:"calls"`
	TotalMicros int64 `json:"total_us"`
	MaxMicros   int64 `json:"max_us"`
}
type evaluationStageMetrics struct {
	mu     sync.Mutex
	totals map[string]evaluationStageTotal
}
type evaluationTimingHandler struct {
	slog.Handler
	metrics *evaluationStageMetrics
}

// A default slog handler forwards to the standard logger. SetDefault redirects
// that standard logger back to slog, so wrapping the original default handler
// creates a recursive bridge and leaves later tests deadlocked. Use an explicit
// sink and restore both logging APIs, including their standard-log settings.
func installEvaluationTimingLogger(metrics *evaluationStageMetrics) func() {
	previousLogger, previousWriter, previousFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(evaluationTimingHandler{Handler: slog.NewTextHandler(previousWriter, nil), metrics: metrics}))
	var once sync.Once
	return func() {
		once.Do(func() { slog.SetDefault(previousLogger); log.SetOutput(previousWriter); log.SetFlags(previousFlags) })
	}
}

func (h evaluationTimingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h evaluationTimingHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == "decision evaluation timings" {
		h.metrics.mu.Lock()
		r.Attrs(func(a slog.Attr) bool {
			if strings.HasSuffix(a.Key, "_us") && a.Value.Kind() == slog.KindInt64 && a.Value.Int64() >= 0 {
				v := h.metrics.totals[a.Key]
				v.Calls++
				v.TotalMicros += a.Value.Int64()
				v.MaxMicros = max(v.MaxMicros, a.Value.Int64())
				h.metrics.totals[a.Key] = v
			}
			return true
		})
		h.metrics.mu.Unlock()
	}
	if h.Handler.Enabled(ctx, r.Level) {
		return h.Handler.Handle(ctx, r)
	}
	return nil
}
func (h evaluationTimingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.Handler = h.Handler.WithAttrs(attrs)
	return h
}
func (h evaluationTimingHandler) WithGroup(name string) slog.Handler {
	h.Handler = h.Handler.WithGroup(name)
	return h
}

type evaluationSQLTotal struct {
	Digest       string `json:"digest"`
	Statement    string `json:"normalized_sql"`
	Calls        uint64 `json:"calls"`
	TimerPicos   uint64 `json:"total_timer_ps"`
	LockPicos    uint64 `json:"total_lock_ps"`
	RowsExamined uint64 `json:"rows_examined"`
}

func evaluationSQLSnapshot(ctx context.Context, db *sql.DB, database string) (map[string]evaluationSQLTotal, error) {
	rows, err := db.QueryContext(ctx, "SELECT DIGEST,DIGEST_TEXT,COUNT_STAR,SUM_TIMER_WAIT,SUM_LOCK_TIME,SUM_ROWS_EXAMINED FROM performance_schema.events_statements_summary_by_digest WHERE SCHEMA_NAME=? AND DIGEST IS NOT NULL ORDER BY DIGEST LIMIT 1001", database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]evaluationSQLTotal{}
	for rows.Next() {
		var v evaluationSQLTotal
		if err := rows.Scan(&v.Digest, &v.Statement, &v.Calls, &v.TimerPicos, &v.LockPicos, &v.RowsExamined); err != nil {
			return nil, err
		}
		out[v.Digest] = v
		if len(out) > 1000 {
			return nil, fmt.Errorf("SQL diagnostic shape budget exceeded")
		}
	}
	return out, rows.Err()
}
func evaluationSQLDelta(before, after map[string]evaluationSQLTotal) ([]evaluationSQLTotal, error) {
	out := []evaluationSQLTotal{}
	for key, v := range after {
		b := before[key]
		if v.Calls < b.Calls || v.TimerPicos < b.TimerPicos || v.LockPicos < b.LockPicos || v.RowsExamined < b.RowsExamined {
			return nil, fmt.Errorf("SQL diagnostics reset during workload")
		}
		v.Calls -= b.Calls
		v.TimerPicos -= b.TimerPicos
		v.LockPicos -= b.LockPicos
		v.RowsExamined -= b.RowsExamined
		if v.Calls > 0 {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TimerPicos == out[j].TimerPicos {
			return out[i].Digest < out[j].Digest
		}
		return out[i].TimerPicos > out[j].TimerPicos
	})
	return out, nil
}

// The digest summary may omit binary-protocol prepared statement bodies.
// Record their command counters explicitly rather than treating the text SQL
// digest list as a complete inventory. These are private-server snapshots.
func evaluationServerSnapshot(ctx context.Context, db *sql.DB) (map[string]uint64, error) {
	rows, err := db.QueryContext(ctx, "SHOW GLOBAL STATUS WHERE Variable_name IN ('Com_stmt_prepare','Com_stmt_execute','Com_stmt_close','Com_select','Com_insert','Com_update','Com_commit','Innodb_row_lock_time','Innodb_row_lock_waits','Threads_running')")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]uint64{}
	for rows.Next() {
		var name string
		var value uint64
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, rows.Err()
}
