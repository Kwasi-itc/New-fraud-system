package mysql

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"testing"
	"time"
)

func TestEvaluationTimingLoggerRestoresStandardBridge(t *testing.T) {
	previousWriter, previousFlags, previousLogger := log.Writer(), log.Flags(), slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousLogger); log.SetOutput(previousWriter); log.SetFlags(previousFlags) })
	var output bytes.Buffer
	log.SetOutput(&output)
	metrics := &evaluationStageMetrics{totals: map[string]evaluationStageTotal{}}
	restore := installEvaluationTimingLogger(metrics)
	slog.Info("decision evaluation timings", "core_us", int64(12))
	log.Print("standard logging while profiling")
	restore()
	restore()
	slog.Info("structured logging after profiling")
	log.Print("standard logging after profiling")
	if slog.Default() != previousLogger || log.Writer() != &output || log.Flags() != previousFlags {
		t.Fatal("logging state was not restored")
	}
	for _, message := range []string{"standard logging while profiling", "structured logging after profiling", "standard logging after profiling"} {
		if !bytes.Contains(output.Bytes(), []byte(message)) {
			t.Fatal("missing logging output", message)
		}
	}
	if metrics.totals["core_us"].Calls != 1 {
		t.Fatal(metrics.totals)
	}
}

func TestEvaluationProfileKeepsTimingsWithoutPayloads(t *testing.T) {
	metrics := &evaluationStageMetrics{totals: map[string]evaluationStageTotal{}}
	handler := evaluationTimingHandler{Handler: slog.NewTextHandler(io.Discard, nil), metrics: metrics}
	for _, duration := range []int64{12, 30} {
		r := slog.NewRecord(time.Now(), slog.LevelDebug, "decision evaluation timings", 0)
		r.AddAttrs(slog.Int64("tx_total_us", duration), slog.String("payload", "must not retain"), slog.String("token_us", "must not retain"), slog.Int64("invalid_us", -1))
		if err := handler.Handle(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	if len(metrics.totals) != 1 || metrics.totals["tx_total_us"] != (evaluationStageTotal{Calls: 2, TotalMicros: 42, MaxMicros: 30}) {
		t.Fatal(metrics.totals)
	}
}

func TestEvaluationSQLProfileSubtractsSetupAndRejectsReset(t *testing.T) {
	before := map[string]evaluationSQLTotal{"a": {Digest: "a", Calls: 5, TimerPicos: 100, LockPicos: 10, RowsExamined: 3}}
	after := map[string]evaluationSQLTotal{"a": {Digest: "a", Calls: 8, TimerPicos: 160, LockPicos: 12, RowsExamined: 8}, "b": {Digest: "b", Calls: 2, TimerPicos: 90}}
	delta, err := evaluationSQLDelta(before, after)
	if err != nil || len(delta) != 2 || delta[0].Digest != "b" || delta[1].Calls != 3 || delta[1].TimerPicos != 60 || delta[1].LockPicos != 2 || delta[1].RowsExamined != 5 {
		t.Fatal(delta, err)
	}
	after["a"] = evaluationSQLTotal{Digest: "a", Calls: 1}
	if _, err := evaluationSQLDelta(before, after); err == nil {
		t.Fatal("counter reset concealed")
	}
}
