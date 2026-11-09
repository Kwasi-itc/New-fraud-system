package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/screeningreceipt"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlpublication"
	"github.com/google/uuid"
)

func TestScreeningReceiptOwner(t *testing.T) {
	f := effectsFixture(t)
	installScreeningOwner(t, f)
	f.db.SetMaxOpenConns(8)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	// Explicit integration authority fixture. Trusted runtime worker identities
	// and restricted accounts remain separate acceptance tasks, D19.21/D19.22.
	authority := func(_ context.Context, _ *sql.Tx, tenant uuid.UUID, _ bool) error {
		if tenant != f.tenant {
			return ErrDecisionAuthorizationDenied
		}
		return nil
	}
	owner, err := mysqlpublication.New(f.db, f.database, f.tenant, authority)
	if err != nil {
		t.Fatal(err)
	}
	intake, err := mysqlpublication.NewIntake(owner, []string{"fixture"})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := screeningreceipt.New(intake)
	if err != nil {
		t.Fatal(err)
	}
	rehash := func(r ports.DispatchRequest) ports.DispatchRequest {
		h := sha256.Sum256(r.Payload)
		r.PayloadSHA256 = hex.EncodeToString(h[:])
		return r
	}
	rekey := func(r ports.DispatchRequest) ports.DispatchRequest {
		var err error
		r.Key, err = dispatchIdentityKey(mysqljobs.Identity{Version: 1, Kind: r.Kind, TenantID: uuid.MustParse(r.TenantID), ResourceID: uuid.MustParse(r.ResourceID), DomainAttempt: r.DomainAttempt, RedriveGeneration: r.RedriveGeneration})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	makeRequest := func() ports.DispatchRequest {
		t.Helper()
		d := decisionInput(f.tenant, f.parent, f.iteration.ID)
		execution := f.screeningInput(d)
		if err := f.store.Mutate(ctx, func(r EffectRepositories) error {
			if _, err := r.Decisions.Create(ctx, d); err != nil {
				return err
			}
			_, err := r.ScreeningExecutions.CreateMany(ctx, []screening.Execution{execution})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(map[string]any{"execution_id": execution.ID, "request": map[string]any{"provider": "fixture", "decision_id": d.ID, "scenario_id": f.parent.String(), "screening_config_id": f.screen.ID, "idempotency_key": execution.ID, "object_type": "account", "object_id": "one", "queries": []map[string]string{{"name": "Name", "type": "Person"}}}})
		if err != nil {
			t.Fatal(err)
		}
		return rekey(rehash(ports.DispatchRequest{Kind: ports.JobScreening, TenantID: f.tenant.String(), ResourceID: execution.ID, DomainAttempt: 1, Payload: raw}))
	}
	count := func(table string, r ports.DispatchRequest) int {
		t.Helper()
		var n int
		if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+table+" WHERE tenant_id=? AND execution_id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(r.ResourceID))).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	r := makeRequest()
	if got, err := provider.Reconcile(ctx, r); err != nil || got.Outcome != ports.DispatchNotFound || ValidateDispatchReceipt(r, got) != nil {
		t.Fatal("absent", got, err)
	}
	t.Run("concurrent_duplicate_and_lost_response", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan ports.DispatchReceipt, 8)
		failures := make(chan error, 8)
		start := make(chan struct{})
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				got, err := provider.Deliver(ctx, r)
				results <- got
				failures <- err
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		var reference string
		for got := range results {
			if got.Outcome != ports.DispatchAccepted || ValidateDispatchReceipt(r, got) != nil {
				t.Fatal(got)
			}
			if reference != "" && got.Reference != reference {
				t.Fatal("duplicate source", got)
			}
			reference = got.Reference
		}
		// Discard every delivery response and construct another client, as on restart.
		restarted, err := screeningreceipt.New(intake)
		if err != nil {
			t.Fatal(err)
		}
		got, err := restarted.Reconcile(ctx, r)
		if err != nil || got.Reference != reference || got.Outcome != ports.DispatchAccepted {
			t.Fatal("lost ack", got, err)
		}
		for _, table := range []string{"screening_intake_keys", "screening_intake_receipts", "screening_screenings"} {
			if n := count(table, r); n != 1 {
				t.Fatal(table, n)
			}
		}
	})
	t.Run("payload_and_identity_conflicts", func(t *testing.T) {
		changed := r
		changed.Payload = append(append([]byte{}, r.Payload...), byte(' '))
		if _, err := provider.Deliver(ctx, changed); !errors.Is(err, mysqlpublication.ErrInvalid) {
			t.Fatal("bad hash", err)
		}
		changed = rehash(changed)
		for _, call := range []func(context.Context, ports.DispatchRequest) (ports.DispatchReceipt, error){provider.Deliver, provider.Reconcile} {
			if _, err := call(ctx, changed); !errors.Is(err, mysqlpublication.ErrConflict) {
				t.Fatal("reused key/body", err)
			}
		}
		wrong := r
		wrong.Kind = ports.JobOutbox
		if _, err := provider.Deliver(ctx, wrong); err == nil {
			t.Fatal("wrong kind")
		}
		wrong = r
		wrong.Key = "decision-v1-" + string(make([]byte, 64))
		if _, err := provider.Deliver(ctx, wrong); err == nil {
			t.Fatal("wrong key")
		}
		wrong = r
		wrong.TenantID = uuid.NewString()
		wrong = rekey(wrong)
		if _, err := provider.Reconcile(ctx, wrong); err == nil {
			t.Fatal("foreign tenant")
		}
	})
	t.Run("new_attempt_retains_source", func(t *testing.T) {
		original, err := provider.Reconcile(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		next := r
		next.DomainAttempt++
		next.RedriveGeneration++
		next = rekey(next)
		got, err := provider.Deliver(ctx, next)
		if err != nil || got.Reference != original.Reference {
			t.Fatal(got, err)
		}
		if count("screening_screenings", r) != 1 || count("screening_intake_receipts", r) != 2 {
			t.Fatal("attempt duplicated business work")
		}
		id := uuid.MustParse(got.Reference)
		version, err := owner.Start(ctx, id, 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = owner.Complete(ctx, id, version, mysqlpublication.Result{Response: []byte(`{}`), Reference: "provider-complete"}); err != nil {
			t.Fatal(err)
		}
		got, err = provider.Deliver(ctx, next)
		if err != nil || got.Reference != original.Reference {
			t.Fatal("terminal receipt", got, err)
		}
		var status string
		if err = f.db.QueryRowContext(ctx, "SELECT status FROM "+schema+".screening_screenings WHERE id=?", mysqlstore.UUID(id)).Scan(&status); err != nil || status != "no_hit" {
			t.Fatal("replay restarted completed search", status, err)
		}
	})
	for _, table := range []string{"screening_execution_projection_queue", "screening_intake_receipts"} {
		t.Run("rollback_"+table, func(t *testing.T) {
			next := makeRequest()
			if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".reject_intake BEFORE INSERT ON "+schema+"."+table+" FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected receipt failure'"); err != nil {
				t.Fatal(err)
			}
			got, err := provider.Deliver(ctx, next)
			if _, dropErr := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".reject_intake"); dropErr != nil {
				t.Fatal(dropErr)
			}
			if err == nil || got.Outcome == ports.DispatchAccepted {
				t.Fatal("provisional success", got, err)
			}
			for _, name := range []string{"screening_screenings", "screening_intake_keys", "screening_intake_receipts"} {
				if count(name, next) != 0 {
					t.Fatal("partial commit", name)
				}
			}
			if got, err := provider.Reconcile(ctx, next); err != nil || got.Outcome != ports.DispatchNotFound {
				t.Fatal(got, err)
			}
			if _, err := provider.Deliver(ctx, next); err != nil {
				t.Fatal("retry after rollback", err)
			}
		})
	}
	t.Run("denied_authority_and_retention", func(t *testing.T) {
		denied, _ := mysqlpublication.New(f.db, f.database, f.tenant, func(context.Context, *sql.Tx, uuid.UUID, bool) error { return ErrDecisionAuthorizationDenied })
		in, _ := mysqlpublication.NewIntake(denied, []string{"fixture"})
		client, _ := screeningreceipt.New(in)
		for _, call := range []func(context.Context, ports.DispatchRequest) (ports.DispatchReceipt, error){client.Deliver, client.Reconcile} {
			if _, err := call(ctx, r); !errors.Is(err, ErrDecisionAuthorizationDenied) {
				t.Fatal(err)
			}
		}
		for _, table := range []string{"screening_intake_keys", "screening_intake_receipts"} {
			if _, err := f.db.ExecContext(ctx, "DELETE FROM "+schema+"."+table+" WHERE tenant_id=? AND execution_id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(r.ResourceID))); err == nil {
				t.Fatal("receipt removed", table)
			}
		}
	})
	t.Run("provider_and_binding_policy", func(t *testing.T) {
		for _, field := range []string{"provider", "decision_id", "idempotency_key", "queries"} {
			next := makeRequest()
			var body struct {
				ExecutionID string         `json:"execution_id"`
				Request     map[string]any `json:"request"`
			}
			if err := json.Unmarshal(next.Payload, &body); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "provider":
				body.Request[field] = "unconfigured"
			case "decision_id":
				body.Request[field] = uuid.NewString()
			case "idempotency_key":
				body.Request[field] = uuid.NewString()
			case "queries":
				body.Request[field] = []any{}
			}
			var err error
			next.Payload, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			next = rehash(next)
			if got, err := provider.Deliver(ctx, next); err == nil || got.Outcome == ports.DispatchAccepted {
				t.Fatal("invalid request accepted", field, got, err)
			}
			for _, table := range []string{"screening_intake_keys", "screening_intake_receipts", "screening_screenings"} {
				if count(table, next) != 0 {
					t.Fatal("invalid binding committed", field, table)
				}
			}
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if got, err := provider.Deliver(canceled, makeRequest()); err == nil || got.Outcome == ports.DispatchAccepted {
			t.Fatal("canceled intake", got, err)
		}
	})
}

func TestScreeningReceiptDispatchHandoff(t *testing.T) {
	f := effectsFixture(t, true, true, true, true)
	installScreeningOwner(t, f)
	f.db.SetMaxOpenConns(8)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	queued, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, f.store.screening, effectQueuePolicies())
	if err != nil {
		t.Fatal(err)
	}
	source, err := mysqlpublication.New(f.db, f.database, f.tenant, func(_ context.Context, _ *sql.Tx, tenant uuid.UUID, _ bool) error {
		if tenant != f.tenant {
			return ErrDecisionAuthorizationDenied
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	intake, err := mysqlpublication.NewIntake(source, []string{"fixture"})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := screeningreceipt.New(intake)
	if err != nil {
		t.Fatal(err)
	}
	sink, err := NewScreeningProjectionSink(queued)
	if err != nil {
		t.Fatal(err)
	}
	projector, err := mysqlpublication.NewProjector(source, f.database, sink)
	if err != nil {
		t.Fatal(err)
	}
	for _, publishFirst := range []bool{false, true} {
		name := "ack_before_publication"
		if publishFirst {
			name = "publication_before_ack"
		}
		t.Run(name, func(t *testing.T) {
			d := decisionInput(f.tenant, f.parent, f.iteration.ID)
			execution := f.screeningInput(d)
			execution.RequestJSON, err = json.Marshal(map[string]any{"provider": "fixture", "decision_id": d.ID, "scenario_id": f.parent.String(), "screening_config_id": f.screen.ID, "idempotency_key": execution.ID, "object_type": "account", "object_id": "one", "queries": []map[string]string{{"name": "Name", "type": "Person"}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := queued.Mutate(ctx, func(r EffectRepositories) error {
				if _, err := r.Decisions.Create(ctx, d); err != nil {
					return err
				}
				_, err := r.ScreeningExecutions.CreateMany(ctx, []screening.Execution{execution})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			delivery, _ := retryStores(t, f, ports.JobScreening)
			dispatch, err := NewEffectDispatchStore(queued, delivery)
			if err != nil {
				t.Fatal(err)
			}
			lease := claimRetryEffect(t, delivery)
			intent, err := dispatch.Prepare(ctx, lease)
			if err != nil {
				t.Fatal(err)
			}
			accepted, err := provider.Deliver(ctx, intent.Request)
			if err != nil {
				t.Fatal(err)
			}
			publish := func() {
				t.Helper()
				id := uuid.MustParse(accepted.Reference)
				version, err := source.Start(ctx, id, 1)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = source.Complete(ctx, id, version, mysqlpublication.Result{Response: []byte(`{}`), Reference: "search-complete"}); err != nil {
					t.Fatal(err)
				}
				result, err := projector.RunBatch(ctx, 10)
				if err != nil || result.Retried != 0 || result.Applied != 1 {
					t.Fatal("projection", result, err)
				}
			}
			if publishFirst {
				publish()
			}
			// Restart the handoff after acceptance, retaining the same leased attempt.
			resumed, err := dispatch.Prepare(ctx, lease)
			if err != nil || !resumed.Resumed || resumed.Request.Key != intent.Request.Key {
				t.Fatal("resume", resumed, err)
			}
			receipt, err := provider.Reconcile(ctx, resumed.Request)
			if err != nil || receipt.Reference != accepted.Reference {
				t.Fatal(receipt, err)
			}
			if publishFirst {
				bad := receipt
				bad.Reference = uuid.NewString()
				if err := dispatch.Complete(ctx, lease, resumed.Request, bad, time.Second); !errors.Is(err, ErrEffectSourceOwned) {
					t.Fatal("foreign source receipt", err)
				}
			}
			if err := dispatch.Complete(ctx, lease, resumed.Request, receipt, time.Second); err != nil {
				t.Fatal("ack", err)
			}
			if committed, err := dispatch.CompletionCommitted(ctx, lease, receipt); err != nil || !committed {
				t.Fatal("completion proof", committed, err)
			}
			if !publishFirst {
				publish()
			}
			var status, reference, state string
			if err := f.db.QueryRowContext(ctx, "SELECT status,provider_reference FROM "+schema+".decision_engine_screening_executions WHERE id=?", mysqlstore.UUID(uuid.MustParse(execution.ID))).Scan(&status, &reference); err != nil || status != "completed" || reference != "search-complete" {
				t.Fatal("newer source overwritten", status, reference, err)
			}
			if err := f.db.QueryRowContext(ctx, "SELECT state FROM "+schema+".decision_engine_effect_job_deliveries WHERE id=?", mysqlstore.UUID(lease.ID)).Scan(&state); err != nil || state != "completed" {
				t.Fatal("delivery", state, err)
			}
		})
	}
}
