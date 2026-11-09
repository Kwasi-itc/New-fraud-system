package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/loadacceptance"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type historyLoadProfile struct {
	ApprovalReference  string                 `json:"approval_reference"`
	Seed               string                 `json:"seed"`
	Rows               int                    `json:"rows"`
	Tenants            int                    `json:"tenants"`
	HotPercent         int                    `json:"hot_percent"`
	ScenariosPerTenant int                    `json:"scenarios_per_tenant"`
	Load               loadacceptance.Profile `json:"load"`
}

func (p historyLoadProfile) validate() error {
	if p.Seed == "" || len(p.Seed) > 128 || p.Rows < 100 || p.Rows > 10000000 || p.Tenants < 2 || p.Tenants > 100 || p.HotPercent < 1 || p.HotPercent > 99 || p.ScenariosPerTenant < 1 || p.ScenariosPerTenant > 100 {
		return errors.New("bounded explicit seed, row/tenant/skew/scenario counts required")
	}
	if p.Rows*(100-p.HotPercent)/100 < p.Tenants-1 {
		return errors.New("every cold tenant requires retained rows")
	}
	return p.Load.Validate()
}
func historyTenant(index int, p historyLoadProfile) int {
	hot := p.Rows * p.HotPercent / 100
	if index < hot {
		return 0
	}
	return 1 + (index-hot)%(p.Tenants-1)
}

// This entry point never treats a proposed profile or smoke run as acceptance.
// The approval reference records an actual agreed target/ticket, not a default.
func TestManualHistoryLoadAcceptance(t *testing.T) {
	path := os.Getenv("DECISION_HISTORY_LOAD_PROFILE")
	if path == "" {
		t.Skip("explicit agreed history profile required")
	}
	if os.Getenv("DECISION_MYSQL_TEST_ADDRESS") != "mysql:3306" || os.Getenv("DECISION_MYSQL_REQUIRE_INTEGRATION") != "1" {
		t.Fatal("private disposable compose fixture required")
	}
	f, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		t.Fatal(e)
	}
	if info.Size() > 64<<10 {
		t.Fatal("profile exceeds 64 KiB")
	}
	var p historyLoadProfile
	decoder := json.NewDecoder(io.LimitReader(f, 64<<10))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&p); e != nil {
		t.Fatal(e)
	}
	var extra any
	if e = decoder.Decode(&extra); e != io.EOF {
		t.Fatal("profile must contain one JSON object", e)
	}
	if strings.TrimSpace(p.ApprovalReference) == "" {
		t.Fatal("record user-agreed target reference before acceptance")
	}
	out := os.Getenv("DECISION_HISTORY_LOAD_OUTPUT")
	if out == "" {
		t.Fatal("new evidence output file required")
	}
	runHistoryLoad(t, p, out, false)
}

func TestHistoryLoadHarnessSmoke(t *testing.T) {
	p := historyLoadProfile{Seed: "history-harness-smoke-v1", Rows: 240, Tenants: 3, HotPercent: 60, ScenariosPerTenant: 2, Load: loadacceptance.Profile{Duration: 600 * time.Millisecond, Rate: 20, Workers: 2, Timeout: 5 * time.Second, P95: 5 * time.Second, P99: 5 * time.Second}}
	path := os.Getenv("DECISION_HISTORY_SMOKE_OUTPUT")
	if path == "" {
		path = filepath.Join(t.TempDir(), "smoke.json")
	}
	runHistoryLoad(t, p, path, true)
}

func runHistoryLoad(t *testing.T, p historyLoadProfile, path string, smoke bool) {
	t.Helper()
	if e := p.validate(); e != nil {
		t.Fatal(e)
	}
	// Refuse overwrites before installing/seeding anything.
	output, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer output.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute+p.Load.Duration)
	defer cancel()
	f := scenarioFoundationDB(t, true, true)
	if !strings.HasPrefix(f.database, "decision_target_") {
		t.Fatal("generated disposable target required")
	}
	f.db.SetMaxOpenConns(8)
	f.db.SetMaxIdleConns(8)
	installIdentityOwner(t, f)
	schema, e := mysqlstore.Identifier(f.database)
	if e != nil {
		t.Fatal(e)
	}
	check := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	namespace := uuid.NewSHA1(uuid.NameSpaceOID, []byte(p.Seed))
	type tenantBinding struct {
		id                  uuid.UUID
		parents, iterations []uuid.UUID
		store               *DecisionStore
	}
	bindings := make([]tenantBinding, p.Tenants)
	counts := make([]int, p.Tenants)
	for i := range bindings {
		b := &bindings[i]
		b.id = uuid.NewSHA1(namespace, []byte(fmt.Sprintf("tenant:%d", i)))
		session := seedIdentity(t, f, b.id, "platform_admin")
		auth, e := SessionAuthorizer(f.database, session)
		check(e)
		authorize := func(ctx context.Context, tx *sql.Tx, id uuid.UUID, write bool) error {
			if write {
				return ErrDecisionAuthorizationDenied
			}
			return auth(ctx, tx, id, AuthoringRead)
		}
		b.store, e = NewDecisionStore(f.db, f.database, b.id, 100, 2<<20, authorize)
		check(e)
		owner := authoringStore(t, f, b.id, 100, 2<<20)
		for j := 0; j < p.ScenariosPerTenant; j++ {
			parent := uuid.NewSHA1(namespace, []byte(fmt.Sprintf("scenario:%d:%d", i, j)))
			check(scenarioInsert(ctx, f.db, f.database, b.id, parent, fmt.Sprintf("Load %d", j)))
			iteration := createAuthoringDraft(t, owner, b.id, parent)
			b.parents = append(b.parents, parent)
			b.iterations = append(b.iterations, uuid.MustParse(iteration.ID))
		}
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Privileged fixture seeding uses 100-row transactions with all real triggers
	// and FKs enabled; no giant transaction or per-row relationship lookup.
	for start := 0; start < p.Rows; start += 100 {
		size := min(100, p.Rows-start)
		marks := make([]string, size)
		args := make([]any, 0, size*11)
		for k := 0; k < size; k++ {
			n := start + k
			tenant := historyTenant(n, p)
			b := bindings[tenant]
			slot := counts[tenant] % len(b.parents)
			counts[tenant]++
			object := fmt.Sprintf("OBJECT-%09d", n)
			if n%10 == 0 {
				object = "HOT"
			}
			marks[k] = "(?,?,?,?,?,?,?,?,?,?,?)"
			args = append(args, mysqlstore.UUID(uuid.NewSHA1(namespace, []byte(fmt.Sprintf("decision:%d", n)))), mysqlstore.UUID(b.id), mysqlstore.UUID(b.parents[slot]), mysqlstore.UUID(b.iterations[slot]), mysqlstore.Text(object), mysqlstore.Text("wallets"), mysqlstore.JSON(`{"fixture":true}`), mysqlstore.Text("review"), 45, true, mysqlstore.DateTime{Time: base.Add(time.Duration(n) * time.Microsecond)})
		}
		_, e = f.db.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,request_body,outcome,score,triggered,created_at) VALUES "+strings.Join(marks, ","), args...)
		check(e)
	}
	for i, b := range bindings {
		check(b.store.ReadHistory(ctx, func(r HistoryRepositories) error {
			n, e := r.Decisions.CountFiltered(ctx, b.id.String(), ports.DecisionListFilter{})
			if e == nil && n != counts[i] {
				return fmt.Errorf("tenant row count %d != %d", n, counts[i])
			}
			return e
		}))
	}
	_, e = f.db.ExecContext(ctx, "ANALYZE TABLE "+schema+".decision_engine_decisions")
	check(e)
	plans := map[string]string{}
	check(bindings[0].store.ReadHistory(ctx, func(r HistoryRepositories) error {
		filters := []ports.DecisionListFilter{{}, {ScenarioID: bindings[0].parents[0].String()}, {ObjectType: "wallets", ObjectID: "HOT"}, {Search: "OBJECT"}}
		for i, filter := range filters {
			where, args, e := r.Decisions.filter(bindings[0].id.String(), filter)
			if e != nil {
				return e
			}
			for _, count := range []bool{false, true} {
				query := r.Decisions.selectSQL(false) + where + " ORDER BY d.created_at DESC,d.id DESC LIMIT 26"
				if count {
					query = "SELECT COUNT(*) FROM " + schema + ".decision_engine_decisions d" + where
				}
				var plan string
				if e = r.Decisions.tx.QueryRowContext(ctx, "EXPLAIN ANALYZE "+query, args...).Scan(&plan); e != nil {
					return e
				}
				plans[fmt.Sprintf("filter_%d_count_%t", i, count)] = plan
			}
		}
		where, args, e := r.Decisions.filter(bindings[0].id.String(), ports.DecisionListFilter{})
		if e != nil {
			return e
		}
		cut := mysqlstore.DateTime{Time: base.Add(time.Duration(p.Rows/2) * time.Microsecond)}
		args = append(args, cut, cut, mysqlstore.UUID(uuid.Nil))
		var plan string
		e = r.Decisions.tx.QueryRowContext(ctx, "EXPLAIN ANALYZE "+r.Decisions.selectSQL(false)+where+" AND (d.created_at<? OR (d.created_at=? AND d.id<?)) ORDER BY d.created_at DESC,d.id DESC LIMIT 26", args...).Scan(&plan)
		plans["cursor"] = plan
		return e
	}))
	type resource struct {
		At     time.Time         `json:"at"`
		Pool   sql.DBStats       `json:"pool"`
		Status map[string]string `json:"mysql_status"`
	}
	resources := []resource{}
	sample := func() error {
		rows, e := f.db.QueryContext(ctx, "SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected','Threads_running','Innodb_buffer_pool_reads','Innodb_buffer_pool_read_requests','Innodb_row_lock_time','Innodb_row_lock_waits','Created_tmp_disk_tables','Bytes_received','Bytes_sent')")
		if e != nil {
			return e
		}
		defer rows.Close()
		v := resource{At: time.Now().UTC(), Pool: f.db.Stats(), Status: map[string]string{}}
		for rows.Next() {
			var name, value string
			if e = rows.Scan(&name, &value); e != nil {
				return e
			}
			v.Status[name] = value
		}
		if e = rows.Err(); e != nil {
			return e
		}
		resources = append(resources, v)
		return nil
	}
	check(sample())
	work := func(ctx context.Context, index int) error {
		tenant := 0
		if index*37%100 >= p.HotPercent {
			tenant = 1 + index%(p.Tenants-1)
		}
		b := bindings[tenant]
		return b.store.ReadHistory(ctx, func(r HistoryRepositories) error {
			filter := ports.DecisionListFilter{}
			var cursor *ports.DecisionListCursor
			switch index % 6 {
			case 1:
				filter.ScenarioID = b.parents[index%len(b.parents)].String()
			case 2:
				filter.ObjectType = "wallets"
				filter.ObjectID = "HOT"
			case 3:
				filter.Search = "OBJECT"
			case 4:
				n, e := r.Decisions.CountFiltered(ctx, b.id.String(), filter)
				if e == nil && n != counts[tenant] {
					return errors.New("count changed")
				}
				return e
			case 5:
				cursor = &ports.DecisionListCursor{CreatedAt: base.Add(time.Duration(p.Rows/2) * time.Microsecond), ID: "ffffffff-ffff-ffff-ffff-ffffffffffff"}
			}
			rows, _, e := r.Decisions.ListFilteredCursor(ctx, b.id.String(), filter, 25, cursor)
			if e != nil {
				return e
			}
			for _, row := range rows {
				if row.TenantID != b.id.String() {
					return errors.New("history tenant isolation failure")
				}
			}
			return nil
		})
	}
	// Sample during the workload as well as at its boundaries. All samples are
	// owned by this goroutine; join the workload before returning on any error.
	type loadResult struct {
		report loadacceptance.Report
		err    error
	}
	done := make(chan loadResult, 1)
	go func() {
		report, err := loadacceptance.Run(ctx, p.Load, work)
		done <- loadResult{report, err}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var result loadResult
	var sampleErr error
collect:
	for {
		select {
		case result = <-done:
			break collect
		case <-ticker.C:
			if err := sample(); err != nil {
				sampleErr = err
				cancel()
				result = <-done
				break collect
			}
		}
	}
	report, runErr := result.report, result.err
	sampleErr = errors.Join(sampleErr, sample())
	artifact := struct {
		Smoke      bool                  `json:"smoke_only"`
		Profile    historyLoadProfile    `json:"profile"`
		TenantRows []int                 `json:"tenant_rows"`
		Plans      map[string]string     `json:"unhinted_plans"`
		Resources  []resource            `json:"resource_snapshots"`
		Load       loadacceptance.Report `json:"load"`
	}{smoke, p, counts, plans, resources, report}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	check(encoder.Encode(artifact))
	check(output.Sync())
	check(errors.Join(runErr, sampleErr))
	t.Logf("history harness: smoke_only=%t seed=%s rows=%d counts=%v offered=%d success=%d p95=%s p99=%s", smoke, p.Seed, p.Rows, counts, report.Offered, report.Succeeded, report.P95, report.P99)
}
