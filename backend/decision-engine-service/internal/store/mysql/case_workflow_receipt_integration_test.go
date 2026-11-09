package mysql

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	casefixture "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/integrationtest"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlworkflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/workflowreceipt"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMySQLCaseWorkflowReceiptActualOwner(t *testing.T) {
	f, unit, _ := combinedFixture(t, true)
	installIdentityOwner(t, f.platformFixture)
	seedIdentity(t, f.platformFixture, f.tenant, "platform_admin")
	installCaseOwner(t, f.platformFixture)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	installCaseWorkflowOwner(t, f)
	inbox, tag := uuid.New(), uuid.New()
	for table, id := range map[string]uuid.UUID{"case_manager_inboxes": inbox, "case_manager_tags": tag} {
		if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+"."+table+"(id,tenant_id,name,active) VALUES(?,?,'workflow',1)", mysqlstore.UUID(id), mysqlstore.UUID(f.tenant)); err != nil {
			t.Fatal(err)
		}
	}
	newDecision := func(object string) string {
		t.Helper()
		d := decisionInput(f.tenant, f.parent, f.iteration.ID)
		d.ObjectID = object
		if err := unit.RunDecisionMutation(ctx, func(s ports.DecisionMutationStore) error { _, err := s.Decisions().Create(ctx, d); return err }); err != nil {
			t.Fatal(err)
		}
		return d.ID
	}
	const token = "workflow-receipt-integration-credential-only"
	user := "case_workflow_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	account := "`" + user + "`@'%'"
	if _, err := f.db.ExecContext(ctx, "CREATE USER "+account+" IDENTIFIED BY 'isolated-case-workflow-only'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.db.ExecContext(context.Background(), "DROP USER "+account); err != nil {
			t.Error(err)
		}
	})
	grants, err := mysqlworkflow.RuntimeGrants(f.database, user)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if _, err := f.db.ExecContext(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	restricted, err := mysqlstore.Open(ctx, platformConfig(f.database, user, "isolated-case-workflow-only"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restricted.Close() })
	owner := casefixture.MySQLWorkflowReceiptOwner(t, restricted, f.database, token, f.tenant)
	server := httptest.NewServer(owner)
	defer server.Close()
	client, err := workflowreceipt.New(workflowreceipt.Config{BaseURL: server.URL, Token: token, AllowHTTP: true, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	request := func(decision, action string) ports.DispatchRequest {
		t.Helper()
		id := uuid.New()
		identity := mysqljobs.Identity{Version: 1, Kind: ports.JobWorkflow, TenantID: f.tenant, ResourceID: id, DomainAttempt: 1}
		key, err := identity.Key()
		if err != nil {
			t.Fatal(err)
		}
		keyJSON, _ := json.Marshal(key)
		digest := sha256.Sum256(keyJSON)
		payload, err := json.Marshal(map[string]any{"execution_id": id, "tenant_id": f.tenant, "decision_id": decision, "scenario_id": f.parent, "action_type": action, "action_config": map[string]any{"inbox_id": inbox, "name": "Workflow receipt", "tag_ids": []uuid.UUID{tag}}})
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(payload)
		return ports.DispatchRequest{Key: "decision-v1-" + hex.EncodeToString(digest[:]), Kind: ports.JobWorkflow, TenantID: f.tenant.String(), ResourceID: id.String(), DomainAttempt: 1, PayloadSHA256: hex.EncodeToString(hash[:]), Payload: payload}
	}
	count := func(table string) int {
		t.Helper()
		var n int
		if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	r := request(newDecision("same-object"), "create_case")
	var accepted ports.DispatchReceipt
	t.Run("lost response reconciles committed case", func(t *testing.T) {
		lost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/internal/v1/workflow-receipts/deliver" {
				rec := httptest.NewRecorder()
				owner.ServeHTTP(rec, r)
				if rec.Code != 200 {
					t.Errorf("owner failed: %d %s", rec.Code, rec.Body.String())
				}
				w.WriteHeader(502)
				return
			}
			owner.ServeHTTP(w, r)
		}))
		defer lost.Close()
		c, err := workflowreceipt.New(workflowreceipt.Config{BaseURL: lost.URL, Token: token, AllowHTTP: true, Timeout: 10 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Deliver(ctx, r); err == nil {
			t.Fatal("lost response accepted")
		}
		accepted, err = c.Reconcile(ctx, r)
		if err != nil || accepted.Outcome != ports.DispatchAccepted {
			t.Fatal(accepted, err)
		}
		if count("case_manager_cases") != 1 || count("case_manager_case_decisions") != 1 || count("case_manager_case_events") != 3 || count("case_manager_outbox_events") != 3 || count("case_manager_intake_receipts") != 3 {
			t.Fatal("non-atomic case outcome")
		}
	})
	t.Run("concurrent replay has one effect", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got, err := client.Deliver(ctx, r)
				if err != nil || got != accepted {
					t.Errorf("replay: %+v %v", got, err)
				}
			}()
		}
		wg.Wait()
		if count("case_manager_cases") != 1 || count("case_manager_outbox_events") != 3 {
			t.Fatal("duplicate effects")
		}
	})
	t.Run("reuse links second decision", func(t *testing.T) {
		next := request(newDecision("same-object"), "add_to_case")
		got, err := client.Deliver(ctx, next)
		if err != nil || got.Reference != accepted.Reference {
			t.Fatal(got, err)
		}
		if count("case_manager_cases") != 1 || count("case_manager_case_decisions") != 2 {
			t.Fatal("failed reuse")
		}
	})
	t.Run("closed case replay preserves membership", func(t *testing.T) {
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".case_manager_cases SET status='closed' WHERE id=?", mysqlstore.UUID(uuid.MustParse(accepted.Reference))); err != nil {
			t.Fatal(err)
		}
		got, err := client.Deliver(ctx, r)
		if err != nil || got != accepted {
			t.Fatal(got, err)
		}
		fresh := request(rDecision(t, r), "create_case")
		got, err = client.Deliver(ctx, fresh)
		if err != nil || got.Reference != accepted.Reference {
			t.Fatal(got, err)
		}
	})
	t.Run("changed body rejects accepted identity", func(t *testing.T) {
		changed := r
		var body map[string]any
		if err := json.Unmarshal(r.Payload, &body); err != nil {
			t.Fatal(err)
		}
		body["action_config"].(map[string]any)["name"] = "changed body"
		changed.Payload, _ = json.Marshal(body)
		bindWorkflowRequest(t, &changed)
		if _, err := client.Deliver(ctx, changed); err == nil {
			t.Fatal("changed body accepted")
		}
		if _, err := client.Reconcile(ctx, changed); err == nil {
			t.Fatal("changed receipt accepted")
		}
	})
	t.Run("new attempt retains original case", func(t *testing.T) {
		next := r
		next.DomainAttempt = 2
		bindWorkflowRequest(t, &next)
		before := count("case_manager_outbox_events")
		got, err := client.Deliver(ctx, next)
		if err != nil || got.Reference != accepted.Reference {
			t.Fatal(got, err)
		}
		if count("case_manager_outbox_events") != before {
			t.Fatal("new attempt repeated business mutation")
		}
	})
	t.Run("foreign tenant cannot deliver or reconcile", func(t *testing.T) {
		foreign := r
		foreign.TenantID = uuid.NewString()
		var body map[string]any
		if err := json.Unmarshal(foreign.Payload, &body); err != nil {
			t.Fatal(err)
		}
		body["tenant_id"] = foreign.TenantID
		foreign.Payload, _ = json.Marshal(body)
		bindWorkflowRequest(t, &foreign)
		if _, err := client.Deliver(ctx, foreign); err == nil {
			t.Fatal("foreign tenant accepted")
		}
		if _, err := client.Reconcile(ctx, foreign); err == nil {
			t.Fatal("foreign tenant receipt disclosed")
		}
	})
	t.Run("restricted account cannot rewrite source or receipt", func(t *testing.T) {
		for _, query := range []string{"UPDATE " + schema + ".decision_engine_decisions SET score=score", "DELETE FROM " + schema + ".case_manager_intake_receipts", "UPDATE " + schema + ".case_manager_intake_receipts SET case_id=case_id", "UPDATE " + schema + ".case_manager_cases SET status='closed'"} {
			if _, err := restricted.ExecContext(ctx, query); err == nil {
				t.Fatal("forbidden write succeeded", query)
			}
		}
	})
	t.Run("missing destination cannot create case", func(t *testing.T) {
		before := count("case_manager_intake_receipts")
		got := request(newDecision("no-existing-case"), "add_to_case")
		if _, err := client.Deliver(ctx, got); err == nil {
			t.Fatal("missing case accepted")
		}
		receipt, err := client.Reconcile(ctx, got)
		if err != nil || receipt.Outcome != ports.DispatchNotFound {
			t.Fatal(receipt, err)
		}
		if count("case_manager_intake_receipts") != before {
			t.Fatal("failed operation committed receipt")
		}
	})
	t.Run("late notification failure rolls everything back", func(t *testing.T) {
		before := count("case_manager_cases")
		receipts := count("case_manager_intake_receipts")
		events := count("case_manager_case_events")
		if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_case_outbox_failure BEFORE INSERT ON "+schema+".case_manager_outbox_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture outbox failure'"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_case_outbox_failure"); err != nil {
				t.Error(err)
			}
		}()
		got := request(newDecision("rollback-object"), "create_case")
		if _, err := client.Deliver(ctx, got); err == nil {
			t.Fatal("failed notification accepted")
		}
		if count("case_manager_cases") != before || count("case_manager_intake_receipts") != receipts || count("case_manager_case_events") != events {
			t.Fatal("partial writes committed")
		}
	})
	t.Run("unauthorized token cannot reconcile", func(t *testing.T) {
		c, err := workflowreceipt.New(workflowreceipt.Config{BaseURL: server.URL, Token: "wrong-workflow-receipt-credential-only", AllowHTTP: true, Timeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Reconcile(ctx, r); err == nil {
			t.Fatal("unauthorized receipt disclosed")
		}
	})
	t.Run("cancelled lock wait cannot commit", func(t *testing.T) {
		next := request(newDecision("cancelled-object"), "create_case")
		raw, _ := json.Marshal([]string{"case-manager-intake", next.TenantID, "workflow.receipt.v1", next.ResourceID})
		hash := sha256.Sum256(raw)
		if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".case_manager_workflow_locks(tenant_id,lock_hash) VALUES(?,?)", mysqlstore.UUID(f.tenant), hash[:]); err != nil {
			t.Fatal(err)
		}
		tx, err := f.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		var locked []byte
		if err := tx.QueryRowContext(ctx, "SELECT lock_hash FROM "+schema+".case_manager_workflow_locks WHERE tenant_id=? AND lock_hash=? FOR UPDATE", mysqlstore.UUID(f.tenant), hash[:]).Scan(&locked); err != nil {
			t.Fatal(err)
		}
		cancelCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
		defer cancel()
		if _, err := client.Deliver(cancelCtx, next); err == nil {
			t.Fatal("cancelled delivery succeeded")
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		got, err := client.Reconcile(ctx, next)
		if err != nil || got.Outcome != ports.DispatchNotFound {
			t.Fatal("cancelled receipt", got, err)
		}
	})
	t.Run("actual PostgreSQL and MySQL case outcomes agree", func(t *testing.T) {
		pg, err := pgxpool.New(ctx, testsupport.PostgresDatabaseURL(t))
		if err != nil {
			t.Fatal(err)
		}
		defer pg.Close()
		fixture := casefixture.WorkflowReceiptOwner(t, pg, token)
		if _, err := pg.Exec(ctx, "INSERT INTO case_manager.tags(id,tenant_id,target,name,color,created_at,updated_at) VALUES($1,$2,'case','workflow','blue',now(),now())", tag, fixture.Tenant); err != nil {
			t.Fatal(err)
		}
		pgServer := httptest.NewServer(fixture.Handler)
		defer pgServer.Close()
		pgClient, err := workflowreceipt.New(workflowreceipt.Config{BaseURL: pgServer.URL, Token: token, AllowHTTP: true, Timeout: 10 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		var targetCase, sourceCase string
		for _, action := range []string{"create_case", "add_to_case", "add_to_case_if_possible"} {
			target := request(newDecision("parity-object"), action)
			sourceDecision := uuid.New()
			if _, err := pg.Exec(ctx, "INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at) SELECT $1,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,now() FROM core.decisions WHERE id=$2", sourceDecision, fixture.Decision); err != nil {
				t.Fatal(err)
			}
			source := target
			source.TenantID = fixture.Tenant.String()
			var body map[string]any
			if err := json.Unmarshal(source.Payload, &body); err != nil {
				t.Fatal(err)
			}
			body["tenant_id"] = fixture.Tenant
			body["decision_id"] = sourceDecision
			body["scenario_id"] = fixture.Scenario
			body["action_config"].(map[string]any)["inbox_id"] = fixture.Inbox
			source.Payload, _ = json.Marshal(body)
			bindWorkflowRequest(t, &source)
			gotTarget, targetErr := client.Deliver(ctx, target)
			gotSource, sourceErr := pgClient.Deliver(ctx, source)
			if targetErr != nil || sourceErr != nil || gotTarget.Outcome != ports.DispatchAccepted || gotSource.Outcome != ports.DispatchAccepted {
				t.Fatal(action, gotTarget, targetErr, gotSource, sourceErr)
			}
			if targetCase == "" {
				targetCase = gotTarget.Reference
				sourceCase = gotSource.Reference
			} else if targetCase != gotTarget.Reference || sourceCase != gotSource.Reference {
				t.Fatal("case reuse differs", action)
			}
			for i := 0; i < 2; i++ {
				replayTarget, e1 := client.Deliver(ctx, target)
				replaySource, e2 := pgClient.Deliver(ctx, source)
				if e1 != nil || e2 != nil || replayTarget != gotTarget || replaySource != gotSource {
					t.Fatal("replay differs", e1, e2)
				}
			}
		}
		var targetName, targetStatus, targetOutcome, sourceName, sourceStatus, sourceOutcome string
		if err := f.db.QueryRowContext(ctx, "SELECT name,status,outcome FROM "+schema+".case_manager_cases WHERE id=?", mysqlstore.UUID(uuid.MustParse(targetCase))).Scan(&targetName, &targetStatus, &targetOutcome); err != nil {
			t.Fatal(err)
		}
		if err := pg.QueryRow(ctx, "SELECT name,status,outcome FROM case_manager.cases WHERE id=$1", sourceCase).Scan(&sourceName, &sourceStatus, &sourceOutcome); err != nil {
			t.Fatal(err)
		}
		if targetName != sourceName || targetStatus != sourceStatus || targetOutcome != sourceOutcome {
			t.Fatal("case values differ")
		}
		for _, table := range []string{"case_decisions", "case_tags", "case_events"} {
			var targetCount, sourceCount int
			if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".case_manager_"+table+" WHERE case_id=?", mysqlstore.UUID(uuid.MustParse(targetCase))).Scan(&targetCount); err != nil {
				t.Fatal(err)
			}
			if err := pg.QueryRow(ctx, "SELECT COUNT(*) FROM case_manager."+table+" WHERE case_id=$1", sourceCase).Scan(&sourceCount); err != nil {
				t.Fatal(err)
			}
			if targetCount != sourceCount {
				t.Fatal("owner writes differ", table, targetCount, sourceCount)
			}
		}
	})
	t.Run("owner process crashes after commit before acknowledgement", func(t *testing.T) {
		next := request(newDecision("process-crash-object"), "create_case")
		before := count("case_manager_cases")
		childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestCaseWorkflowCrashSubprocess$")
		cmd.Env = append(os.Environ(), "CASE_WORKFLOW_CRASH_HELPER=1", "CASE_WORKFLOW_CRASH_DATABASE="+f.database, "CASE_WORKFLOW_CRASH_USER="+user, "CASE_WORKFLOW_CRASH_TENANT="+f.tenant.String())
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		ready := make(chan string, 1)
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				if strings.HasPrefix(scanner.Text(), "CASE_WORKFLOW_URL=") {
					ready <- strings.TrimPrefix(scanner.Text(), "CASE_WORKFLOW_URL=")
					return
				}
			}
			ready <- ""
		}()
		var address string
		select {
		case address = <-ready:
		case <-childCtx.Done():
		}
		if address == "" {
			cancel()
			_ = cmd.Wait()
			t.Fatal("crash owner did not start")
		}
		crashClient, err := workflowreceipt.New(workflowreceipt.Config{BaseURL: address, Token: token, AllowHTTP: true, Timeout: 10 * time.Second})
		if err != nil {
			cancel()
			_ = cmd.Wait()
			t.Fatal(err)
		}
		_, sendErr := crashClient.Deliver(ctx, next)
		err = cmd.Wait()
		exit, ok := err.(*exec.ExitError)
		if sendErr == nil || !ok || exit.ExitCode() != 23 {
			t.Fatal("expected committed-owner crash", sendErr, err)
		}
		// Reconstruct the owner on a fresh pool: no process memory or HTTP response
		// is used to establish acceptance after the crash.
		reopened, err := mysqlstore.Open(ctx, platformConfig(f.database, user, "isolated-case-workflow-only"))
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		restarted := httptest.NewServer(casefixture.MySQLWorkflowReceiptOwner(t, reopened, f.database, token, f.tenant))
		defer restarted.Close()
		recovered, err := workflowreceipt.New(workflowreceipt.Config{BaseURL: restarted.URL, Token: token, AllowHTTP: true, Timeout: 10 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := recovered.Reconcile(ctx, next)
		if err != nil || receipt.Outcome != ports.DispatchAccepted {
			t.Fatal(receipt, err)
		}
		replay, err := recovered.Deliver(ctx, next)
		if err != nil || replay != receipt {
			t.Fatal(replay, err)
		}
		if count("case_manager_cases") != before+1 {
			t.Fatal("crash/restart duplicated case")
		}
	})
	t.Run("persisted decision workflow intent and completion use real case receipt", func(t *testing.T) {
		config, err := NewConfigurationStore(f.db, f.database, f.tenant, 500, 2<<20, allowAuthoringFixture)
		if err != nil {
			t.Fatal(err)
		}
		definition := f.wf
		definition.ID = uuid.NewString()
		definition.Name = "Case dispatch"
		definition.ActionType = workflow.ActionTypeCreateCase
		definition.ActionConfig, _ = json.Marshal(map[string]any{"inbox_id": inbox, "name": "Queued case"})
		if err := config.Mutate(ctx, func(r ConfigurationRepositories) error { _, err := r.Workflows.Create(ctx, definition); return err }); err != nil {
			t.Fatal(err)
		}
		d := decisionInput(f.tenant, f.parent, f.iteration.ID)
		d.ObjectID = "queued-case-object"
		effect := workflow.Execution{ID: uuid.NewString(), TenantID: f.tenant.String(), WorkflowID: &definition.ID, DecisionID: d.ID, ScenarioID: f.parent.String(), ActionType: definition.ActionType, ActionConfig: definition.ActionConfig, Status: workflow.ExecutionStatusPendingDispatch, CreatedAt: d.CreatedAt}
		if err := unit.RunDecisionMutation(ctx, func(s ports.DecisionMutationStore) error {
			if _, err := s.Decisions().Create(ctx, d); err != nil {
				return err
			}
			_, err := s.WorkflowExecutions().CreateMany(ctx, []workflow.Execution{effect})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		delivery, err := NewEffectDeliveryStore(f.db, f.database, f.tenant, ports.JobWorkflow, effectQueuePolicies()[ports.JobWorkflow].Name, time.Minute, allowEffectWorkerFixture)
		if err != nil {
			t.Fatal(err)
		}
		dispatch, err := NewEffectDispatchStore(unit.effects, delivery)
		if err != nil {
			t.Fatal(err)
		}
		lease, err := dispatch.Claim(ctx)
		if err != nil || lease == nil {
			t.Fatal(lease, err)
		}
		intent, err := dispatch.Prepare(ctx, *lease)
		if err != nil {
			t.Fatal(err)
		}
		before := count("case_manager_cases")
		receipt, err := client.Deliver(ctx, intent.Request)
		if err != nil || receipt.Outcome != ports.DispatchAccepted {
			t.Fatal(receipt, err)
		}
		// Simulate a decision-worker stop after the real case owner committed,
		// before local completion. Expire its lease without changing the intent.
		stale := *lease
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_effect_job_deliveries SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=? AND lease_token=?", mysqlstore.UUID(lease.ID), mysqlstore.UUID(lease.Token)); err != nil {
			t.Fatal(err)
		}
		maintenance, err := NewReceiptAwareEffectMaintenance(delivery)
		if err != nil {
			t.Fatal(err)
		}
		if n, _, err := maintenance.MaintainPage(ctx, 10, false, nil); err != nil || n != 1 {
			t.Fatal("recovery maintenance", n, err)
		}
		// Advance only this fixture's retry deadline; no production clock changes.
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_effect_job_deliveries SET available_at=UTC_TIMESTAMP(6) WHERE id=? AND state='available'", mysqlstore.UUID(stale.ID)); err != nil {
			t.Fatal(err)
		}
		dispatch, err = NewEffectDispatchStore(unit.effects, delivery)
		if err != nil {
			t.Fatal(err)
		}
		lease, err = dispatch.Claim(ctx)
		if err != nil || lease == nil || lease.Token == stale.Token || lease.Attempt != stale.Attempt+1 {
			t.Fatal("recovery claim", lease, err)
		}
		if err := dispatch.Complete(ctx, stale, intent.Request, receipt, time.Second); !errors.Is(err, ErrStaleEffectLease) {
			t.Fatal("stale worker completion accepted", err)
		}
		resumed, err := dispatch.Prepare(ctx, *lease)
		if err != nil || !resumed.Resumed || resumed.Request.Key != intent.Request.Key || resumed.Request.PayloadSHA256 != intent.Request.PayloadSHA256 {
			t.Fatal("recovery lost original intent", resumed, err)
		}
		reconciled, err := client.Reconcile(ctx, resumed.Request)
		if err != nil || reconciled != receipt {
			t.Fatal("recovery did not find accepted case", reconciled, err)
		}
		if err := dispatch.Complete(ctx, *lease, resumed.Request, reconciled, time.Second); err != nil {
			t.Fatal(err)
		}
		committed, err := dispatch.CompletionCommitted(ctx, *lease, receipt)
		if err != nil || !committed {
			t.Fatal("completion proof", committed, err)
		}
		if count("case_manager_cases") != before+1 {
			t.Fatal("missing case effect")
		}
		if next, err := dispatch.Claim(ctx); err != nil || next != nil {
			t.Fatal("completed workflow requeued", next, err)
		}
	})
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprintf("uncertain database commit committed=%t", committed), func(t *testing.T) {
			cfg := platformConfig(f.database, user, "isolated-case-workflow-only")
			address, fired := commitFaultProxy(t, cfg.Address, committed)
			cfg.Address = address
			faultDB, err := mysqlstore.Open(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer faultDB.Close()
			faultOwner := httptest.NewServer(casefixture.MySQLWorkflowReceiptOwner(t, faultDB, f.database, token, f.tenant))
			defer faultOwner.Close()
			faultClient, err := workflowreceipt.New(workflowreceipt.Config{BaseURL: faultOwner.URL, Token: token, AllowHTTP: true, Timeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			r := request(newDecision(uuid.NewString()), "create_case")
			before := map[string]int{}
			for _, table := range []string{"case_manager_cases", "case_manager_case_decisions", "case_manager_case_events", "case_manager_outbox_events", "case_manager_intake_receipts"} {
				before[table] = count(table)
			}
			if _, err := faultClient.Deliver(ctx, r); err == nil {
				t.Fatal("uncertain COMMIT returned success")
			}
			select {
			case <-fired:
			case <-time.After(time.Second):
				t.Fatal("COMMIT fault was not injected")
			}
			// A different owner pool reads the durable outcome after the broken
			// connection. Never infer absence from the failed COMMIT response.
			receipt, err := client.Reconcile(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			want := ports.DispatchNotFound
			if committed {
				want = ports.DispatchAccepted
			}
			if receipt.Outcome != want {
				t.Fatal(receipt)
			}
			if !committed {
				for table, n := range before {
					if count(table) != n {
						t.Fatalf("rolled back transaction changed %s", table)
					}
				}
			}
			accepted, err := client.Deliver(ctx, r)
			if err != nil || accepted.Outcome != ports.DispatchAccepted {
				t.Fatal(accepted, err)
			}
			if committed && accepted != receipt {
				t.Fatal("replay changed accepted receipt")
			}
			for i := 0; i < 2; i++ {
				got, err := client.Deliver(ctx, r)
				if err != nil || got != accepted {
					t.Fatal(got, err)
				}
			}
			for table, delta := range map[string]int{"case_manager_cases": 1, "case_manager_case_decisions": 1, "case_manager_case_events": 3, "case_manager_outbox_events": 3, "case_manager_intake_receipts": 3} {
				if count(table) != before[table]+delta {
					t.Fatalf("missing or duplicate effect in %s", table)
				}
			}
		})
	}
	t.Run("bounded tag batches preserve membership audit and rollback", func(t *testing.T) {
		tags := []uuid.UUID{tag}
		for i := 1; i < 120; i++ {
			id := uuid.New()
			tags = append(tags, id)
			if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".case_manager_tags(id,tenant_id,name,active) VALUES(?,?,'batch tag',1)", mysqlstore.UUID(id), mysqlstore.UUID(f.tenant)); err != nil {
				t.Fatal(err)
			}
		}
		tags = append(tags, tag)
		withTags := func(tags []uuid.UUID) ports.DispatchRequest {
			t.Helper()
			r := request(newDecision(uuid.NewString()), "create_case")
			var body map[string]any
			if err := json.Unmarshal(r.Payload, &body); err != nil {
				t.Fatal(err)
			}
			body["action_config"].(map[string]any)["tag_ids"] = tags
			r.Payload, _ = json.Marshal(body)
			bindWorkflowRequest(t, &r)
			return r
		}
		r := withTags(tags)
		receipt, err := client.Deliver(ctx, r)
		if err != nil || receipt.Outcome != ports.DispatchAccepted {
			t.Fatal(receipt, err)
		}
		for table, want := range map[string]int{"case_manager_case_tags": 120, "case_manager_case_events": 123} {
			var n int
			if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+table+" WHERE case_id=?", mysqlstore.UUID(uuid.MustParse(receipt.Reference))).Scan(&n); err != nil || n != want {
				t.Fatal(table, n, err)
			}
		}
		before := count("case_manager_cases")
		missing := append(append([]uuid.UUID(nil), tags...), uuid.New())
		for _, invalid := range [][]uuid.UUID{missing, make([]uuid.UUID, 501)} {
			r := withTags(invalid)
			if _, err := client.Deliver(ctx, r); err == nil {
				t.Fatal("invalid tag batch accepted")
			}
			got, err := client.Reconcile(ctx, r)
			if err != nil || got.Outcome != ports.DispatchNotFound {
				t.Fatal(got, err)
			}
		}
		if count("case_manager_cases") != before {
			t.Fatal("invalid batch created case")
		}
	})
}

func installCaseWorkflowOwner(t *testing.T, target effectFixture) migrations.Plan {
	t.Helper()
	ctx := context.Background()
	ref, _, _ := combinedFixture(t, true)
	installIdentityOwner(t, ref.platformFixture)
	source, err := mysqlworkflow.CombinedStatements(ref.database, "root")
	if err != nil {
		t.Fatal(err)
	}
	steps, err := mysqlworkflow.CombinedStatements(target.database, "root")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := ref.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for i, step := range source {
		if strings.HasPrefix(step.SQL, "ALTER ") {
			before, err := migrations.Definition(ctx, conn, ref.database, step.Kind, step.Name)
			if err != nil {
				t.Fatal(err)
			}
			steps[i].Before = strings.ReplaceAll(before, ref.database, target.database)
		}
		if _, err := conn.ExecContext(ctx, step.SQL); err != nil {
			t.Fatal(step.ID, err)
		}
		after, err := migrations.Definition(ctx, conn, ref.database, step.Kind, step.Name)
		if err != nil {
			t.Fatal(err)
		}
		steps[i].After = strings.ReplaceAll(after, ref.database, target.database)
	}
	plan, err := mysqlworkflow.Plan(target.database, "root", steps)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := migrations.NewForOwner(target.db, "case_manager", plan)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := runner.Apply(ctx); err != nil {
			t.Fatal("case access to workflow upgrade", err)
		}
	}
	if err := runner.Verify(ctx, nil); err != nil {
		t.Fatal("case workflow definitions", err)
	}
	fresh, _, _ := combinedFixture(t, true)
	installIdentityOwner(t, fresh.platformFixture)
	freshSteps, err := mysqlworkflow.CombinedStatements(fresh.database, "root")
	if err != nil {
		t.Fatal(err)
	}
	for i := range freshSteps {
		freshSteps[i].Before = strings.ReplaceAll(steps[i].Before, target.database, fresh.database)
		freshSteps[i].After = strings.ReplaceAll(steps[i].After, target.database, fresh.database)
	}
	freshPlan, err := mysqlworkflow.Plan(fresh.database, "root", freshSteps)
	if err != nil {
		t.Fatal(err)
	}
	freshRunner, err := migrations.NewForOwner(fresh.db, "case_manager", freshPlan)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := freshRunner.Apply(ctx); err != nil {
			t.Fatal("fresh case workflow install", err)
		}
	}
	if err := freshRunner.Verify(ctx, nil); err != nil {
		t.Fatal(err)
	}
	freshSchema, _ := mysqlstore.Identifier(fresh.database)
	if _, err := fresh.db.ExecContext(ctx, "ALTER TABLE "+freshSchema+".case_manager_cases ADD COLUMN fixture_drift INT"); err != nil {
		t.Fatal(err)
	}
	if err := freshRunner.Verify(ctx, nil); err == nil {
		t.Fatal("workflow schema drift accepted")
	}
	return plan
}

func TestCaseWorkflowCrashSubprocess(t *testing.T) {
	if os.Getenv("CASE_WORKFLOW_CRASH_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	if os.Getenv("DECISION_MYSQL_TEST_ADDRESS") != "mysql:3306" {
		t.Fatal("private fixture required")
	}
	database := os.Getenv("CASE_WORKFLOW_CRASH_DATABASE")
	user := os.Getenv("CASE_WORKFLOW_CRASH_USER")
	tenant := uuid.MustParse(os.Getenv("CASE_WORKFLOW_CRASH_TENANT"))
	if !strings.HasPrefix(database, "decision_target_") || !strings.HasPrefix(user, "case_workflow_") {
		t.Fatal("generated fixture identity required")
	}
	db, err := mysqlstore.Open(context.Background(), platformConfig(database, user, "isolated-case-workflow-only"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	owner := casefixture.MySQLWorkflowReceiptOwner(t, db, database, "workflow-receipt-integration-credential-only", tenant)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		owner.ServeHTTP(rec, r)
		if rec.Code != 200 {
			os.Exit(24)
		}
		os.Exit(23)
	}))
	defer server.Close()
	fmt.Println("CASE_WORKFLOW_URL=" + server.URL)
	<-context.Background().Done()
}

func bindWorkflowRequest(t *testing.T, r *ports.DispatchRequest) {
	t.Helper()
	identity := mysqljobs.Identity{Version: 1, Kind: r.Kind, TenantID: uuid.MustParse(r.TenantID), ResourceID: uuid.MustParse(r.ResourceID), DomainAttempt: r.DomainAttempt, RedriveGeneration: r.RedriveGeneration}
	key, err := identity.Key()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	r.Key = "decision-v1-" + hex.EncodeToString(hash[:])
	hash = sha256.Sum256(r.Payload)
	r.PayloadSHA256 = hex.EncodeToString(hash[:])
}

func rDecision(t *testing.T, r ports.DispatchRequest) string {
	t.Helper()
	var body struct {
		Decision string `json:"decision_id"`
	}
	if err := json.Unmarshal(r.Payload, &body); err != nil {
		t.Fatal(err)
	}
	return body.Decision
}
