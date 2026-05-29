package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
)

func TestScanScreening(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	sentAt := now.Add(time.Minute)
	completedAt := now.Add(2 * time.Minute)
	failedAt := now.Add(3 * time.Minute)
	decisionID := "decision-1"
	scenarioID := "scenario-1"
	configID := "config-1"
	counterparty := "cp-1"

	item, err := scanScreening(fakeScanner{values: []any{
		"screening-1",
		"tenant-1",
		decisionID,
		scenarioID,
		configID,
		"idem-1",
		"opensanctions",
		"business",
		"obj-1",
		string(screening.StatusAwaitingReview),
		json.RawMessage(`{"query":true}`),
		json.RawMessage(`{"hits":1}`),
		"provider-ref",
		"",
		true,
		false,
		true,
		counterparty,
		now,
		now,
		sentAt,
		completedAt,
		failedAt,
	}})
	if err != nil {
		t.Fatalf("scanScreening returned error: %v", err)
	}

	if item.DecisionID != decisionID || item.ScenarioID != scenarioID || item.ScreeningConfigID != configID {
		t.Fatalf("expected optional ids to be populated, got %#v", item)
	}
	if item.IdempotencyKey != "idem-1" {
		t.Fatalf("expected idempotency key idem-1, got %q", item.IdempotencyKey)
	}
	if item.Status != screening.StatusAwaitingReview {
		t.Fatalf("expected awaiting_review, got %s", item.Status)
	}
	if item.UniqueCounterpartyIdentifier == nil || *item.UniqueCounterpartyIdentifier != counterparty {
		t.Fatalf("expected counterparty id to be set, got %#v", item.UniqueCounterpartyIdentifier)
	}
	if item.SentAt == nil || !item.SentAt.Equal(sentAt) || item.CompletedAt == nil || !item.CompletedAt.Equal(completedAt) || item.FailedAt == nil || !item.FailedAt.Equal(failedAt) {
		t.Fatalf("expected timestamps to be set, got %#v", item)
	}
}

func TestScanMatch(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	counterparty := "cp-1"

	item, err := scanMatch(fakeScanner{values: []any{
		"match-1",
		"tenant-1",
		"screening-1",
		"entity-1",
		"opensanctions",
		string(screening.MatchStatusPending),
		"Acme Corp",
		0.91,
		json.RawMessage(`{"entity":"x"}`),
		"{alpha,beta}",
		counterparty,
		true,
		now,
		now,
	}})
	if err != nil {
		t.Fatalf("scanMatch returned error: %v", err)
	}

	if item.Status != screening.MatchStatusPending {
		t.Fatalf("expected pending status, got %s", item.Status)
	}
	if len(item.MatchedTexts) != 2 || item.MatchedTexts[0] != "alpha" || item.MatchedTexts[1] != "beta" {
		t.Fatalf("expected matched texts to be populated, got %#v", item.MatchedTexts)
	}
	if item.UniqueCounterpartyIdentifier == nil || *item.UniqueCounterpartyIdentifier != counterparty {
		t.Fatalf("expected counterparty id, got %#v", item.UniqueCounterpartyIdentifier)
	}
}

func TestScanContinuousConfig(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	inboxID := "inbox-1"

	item, err := scanContinuousConfig(fakeScanner{values: []any{
		"cfg-1",
		"tenant-1",
		"PEP Watch",
		"business",
		"opensanctions",
		json.RawMessage(`{"name":"name"}`),
		inboxID,
		true,
		now,
		now,
	}})
	if err != nil {
		t.Fatalf("scanContinuousConfig returned error: %v", err)
	}
	if item.ReviewInboxID == nil || *item.ReviewInboxID != inboxID {
		t.Fatalf("expected review inbox id, got %#v", item.ReviewInboxID)
	}
}

func TestScanMonitoredObject(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	lastScreenedAt := now.Add(time.Minute)

	item, err := scanMonitoredObject(fakeScanner{values: []any{
		"mo-1",
		"tenant-1",
		"cfg-1",
		"business",
		"obj-1",
		string(screening.MonitoredObjectStatusActive),
		json.RawMessage(`{"name":"Acme"}`),
		now,
		now,
		lastScreenedAt,
	}})
	if err != nil {
		t.Fatalf("scanMonitoredObject returned error: %v", err)
	}
	if item.Status != screening.MonitoredObjectStatusActive {
		t.Fatalf("expected active status, got %s", item.Status)
	}
	if item.LastScreenedAt == nil || !item.LastScreenedAt.Equal(lastScreenedAt) {
		t.Fatalf("expected last screened at, got %#v", item.LastScreenedAt)
	}
}

func TestScanDatasetUpdateJob(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(time.Minute)
	completedAt := now.Add(2 * time.Minute)

	item, err := scanDatasetUpdateJob(fakeScanner{values: []any{
		"job-1",
		"tenant-1",
		"opensanctions",
		"rescreen_monitored_objects",
		string(screening.DatasetUpdateJobStatusCompleted),
		"cursor-1",
		json.RawMessage(`{"count":2}`),
		"",
		3,
		now,
		now,
		startedAt,
		completedAt,
	}})
	if err != nil {
		t.Fatalf("scanDatasetUpdateJob returned error: %v", err)
	}
	if item.Status != screening.DatasetUpdateJobStatusCompleted {
		t.Fatalf("expected completed status, got %s", item.Status)
	}
	if item.StartedAt == nil || !item.StartedAt.Equal(startedAt) || item.CompletedAt == nil || !item.CompletedAt.Equal(completedAt) {
		t.Fatalf("expected lifecycle timestamps, got %#v", item)
	}
}

func TestScreeningWhitelistRepositorySearchBuildsExpectedQuery(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	entityID := "entity-1"
	counterparty := "cp-1"
	q := &fakeQueryable{
		rows: &fakeRows{rows: [][]any{{
			"whitelist-1",
			"tenant-1",
			entityID,
			counterparty,
			"reviewer-1",
			now,
		}}, idx: -1},
	}

	repo := NewScreeningWhitelistRepository(q)
	items, err := repo.Search(context.Background(), "tenant-1", &entityID, &counterparty)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if !strings.Contains(q.lastQuerySQL, "and entity_id = $2") {
		t.Fatalf("expected entity filter in query, got %s", q.lastQuerySQL)
	}
	if !strings.Contains(q.lastQuerySQL, "and unique_counterparty_identifier = $3") {
		t.Fatalf("expected counterparty filter in query, got %s", q.lastQuerySQL)
	}
	if len(q.lastQueryArgs) != 3 || q.lastQueryArgs[0] != "tenant-1" || q.lastQueryArgs[1] != entityID || q.lastQueryArgs[2] != counterparty {
		t.Fatalf("unexpected query args: %#v", q.lastQueryArgs)
	}
}

func TestScreeningMatchRepositoryReplaceForScreeningDeletesThenInserts(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	counterparty := "cp-1"
	q := &fakeQueryable{}
	repo := NewScreeningMatchRepository(q)

	err := repo.ReplaceForScreening(context.Background(), "screening-1", []screening.Match{
		{
			ID:                           "match-1",
			TenantID:                     "tenant-1",
			ScreeningID:                  "screening-1",
			EntityID:                     "entity-1",
			Provider:                     "opensanctions",
			Status:                       screening.MatchStatusPending,
			Name:                         "Acme Corp",
			Score:                        0.77,
			Payload:                      json.RawMessage(`{"hit":true}`),
			MatchedTexts:                 []string{"Acme"},
			UniqueCounterpartyIdentifier: &counterparty,
			Enriched:                     true,
			CreatedAt:                    now,
			UpdatedAt:                    now,
		},
	})
	if err != nil {
		t.Fatalf("ReplaceForScreening returned error: %v", err)
	}

	if len(q.execCalls) != 2 {
		t.Fatalf("expected 2 exec calls, got %d", len(q.execCalls))
	}
	if !strings.Contains(q.execCalls[0].sql, "delete from screening.screening_matches") {
		t.Fatalf("expected first statement to be delete, got %s", q.execCalls[0].sql)
	}
	if q.execCalls[0].args[0] != "screening-1" {
		t.Fatalf("expected delete args to target screening-1, got %#v", q.execCalls[0].args)
	}
	if !strings.Contains(q.execCalls[1].sql, "insert into screening.screening_matches") {
		t.Fatalf("expected second statement to be insert, got %s", q.execCalls[1].sql)
	}
	arrayArg, ok := q.execCalls[1].args[9].(*pq.StringArray)
	if !ok {
		t.Fatalf("expected pq array insert arg, got %T", q.execCalls[1].args[9])
	}
	if len(*arrayArg) != 1 || (*arrayArg)[0] != "Acme" {
		t.Fatalf("expected matched texts array, got %#v", *arrayArg)
	}
}

func TestScreeningMatchRepositoryReplaceForScreeningStopsOnDeleteError(t *testing.T) {
	q := &fakeQueryable{execErrs: []error{errors.New("delete failed")}}
	repo := NewScreeningMatchRepository(q)

	err := repo.ReplaceForScreening(context.Background(), "screening-1", []screening.Match{{ID: "match-1"}})
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(q.execCalls) != 1 {
		t.Fatalf("expected only delete call when delete fails, got %d", len(q.execCalls))
	}
}

func TestHelpers(t *testing.T) {
	if got := nullIfEmpty(""); got != nil {
		t.Fatalf("expected nil for empty string, got %#v", got)
	}
	if got := nullIfEmpty("value"); got == nil || *got != "value" {
		t.Fatalf("expected pointer to value, got %#v", got)
	}
	if got := itoa(42); got != "42" {
		t.Fatalf("expected 42, got %q", got)
	}
}

type fakeScanner struct {
	values []any
}

func (f fakeScanner) Scan(dest ...any) error {
	if len(dest) != len(f.values) {
		return errors.New("destination count mismatch")
	}
	for i := range dest {
		if err := assignScanValue(dest[i], f.values[i]); err != nil {
			return err
		}
	}
	return nil
}

type fakeRows struct {
	rows   [][]any
	idx    int
	closed bool
	err    error
}

func (f *fakeRows) Close()                                       { f.closed = true }
func (f *fakeRows) Err() error                                   { return f.err }
func (f *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeRows) Values() ([]any, error) {
	if f.idx < 0 || f.idx >= len(f.rows) {
		return nil, errors.New("no current row")
	}
	return f.rows[f.idx], nil
}
func (f *fakeRows) RawValues() [][]byte { return nil }
func (f *fakeRows) Next() bool {
	if f.idx+1 >= len(f.rows) {
		f.closed = true
		return false
	}
	f.idx++
	return true
}
func (f *fakeRows) Scan(dest ...any) error {
	if f.idx < 0 || f.idx >= len(f.rows) {
		return errors.New("no current row")
	}
	return fakeScanner{values: f.rows[f.idx]}.Scan(dest...)
}

type fakeRow struct {
	values []any
	err    error
}

func (f fakeRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	return fakeScanner{values: f.values}.Scan(dest...)
}

type execCall struct {
	sql  string
	args []any
}

type fakeQueryable struct {
	rows          pgx.Rows
	row           pgx.Row
	queryErr      error
	execErrs      []error
	execCalls     []execCall
	lastQuerySQL  string
	lastQueryArgs []any
}

func (f *fakeQueryable) Exec(_ context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	f.execCalls = append(f.execCalls, execCall{sql: sql, args: arguments})
	if len(f.execErrs) > 0 {
		err := f.execErrs[0]
		f.execErrs = f.execErrs[1:]
		if err != nil {
			return pgconn.CommandTag{}, err
		}
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeQueryable) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.lastQuerySQL = sql
	f.lastQueryArgs = args
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if f.rows != nil {
		return f.rows, nil
	}
	return &fakeRows{idx: -1}, nil
}

func (f *fakeQueryable) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	if f.row != nil {
		return f.row
	}
	return fakeRow{}
}

type valueScanner interface {
	Scan(src any) error
}

func assignScanValue(dest, src any) error {
	if scanner, ok := dest.(valueScanner); ok {
		return scanner.Scan(src)
	}

	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return errors.New("destination must be non-nil pointer")
	}
	target := rv.Elem()
	if src == nil {
		target.SetZero()
		return nil
	}

	srcValue := reflect.ValueOf(src)
	if target.Kind() == reflect.Ptr {
		elem := reflect.New(target.Type().Elem())
		if err := setAssignableValue(elem.Elem(), srcValue); err != nil {
			return err
		}
		target.Set(elem)
		return nil
	}

	return setAssignableValue(target, srcValue)
}

func setAssignableValue(target, src reflect.Value) error {
	if src.Type().AssignableTo(target.Type()) {
		target.Set(src)
		return nil
	}
	if src.Type().ConvertibleTo(target.Type()) {
		target.Set(src.Convert(target.Type()))
		return nil
	}
	return errors.New("value is not assignable")
}
