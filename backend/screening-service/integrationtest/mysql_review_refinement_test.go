package integrationtest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlreview"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	d "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	runtime "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	native "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/localmatching"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlpublication"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestMySQLScreeningReviewRefinementAndCounterpartyJourney(t *testing.T) {
	f := fixtureMySQL(t)
	ctx := f.ctx
	p := f.principal()
	schema := "`" + f.database + "`."
	apiDB := f.runtimeDB(t, "api")
	workerDB := f.runtimeDB(t, "worker")
	apiStore, e := native.New(apiDB, f.database)
	must(t, e)
	worker, e := native.New(workerDB, f.database)
	must(t, e)
	svc := fixtureService(f)
	publication, e := mysqlpublication.New(apiDB, f.database, f.tenant, func(_ context.Context, _ *sql.Tx, tenant uuid.UUID, _ bool) error {
		if tenant != f.tenant {
			return identity.Forbidden()
		}
		return nil
	})
	must(t, e)
	intake, e := mysqlpublication.NewIntake(publication, []string{"opensanctions"})
	must(t, e)
	dispatch := service.NewDispatchService(worker, clock{}, worker.Screenings(), worker.ScreeningMatches(), runtime.LocalProvider{}, nil)
	reviewer, e := mysqlreview.New(f.db, f.database, auth.Session{ActorID: f.user, TenantID: f.tenant, SessionID: f.sid, MembershipVersion: 1})
	must(t, e)
	create := func(partial bool, cp *string) (d.Screening, uuid.UUID, uuid.UUID) {
		_, renameErr := f.db.ExecContext(ctx, "UPDATE "+schema+"decision_engine_scenarios SET name=CONCAT('Synthetic fixture ',BIN_TO_UUID(id)) WHERE tenant_id=?", mysqlstore.UUID(f.tenant))
		must(t, renameErr)
		decision, scenario, config, execution := seedDecision(t, f)
		request := d.SearchRequest{Provider: "opensanctions", DecisionID: decision.String(), ScenarioID: scenario.String(), ScreeningConfigID: config.String(), IdempotencyKey: execution.String(), ObjectType: "Person", ObjectID: "synthetic-person", Queries: []d.SearchQuery{{Name: "Alex Example", Type: "Person"}}, UniqueCounterpartyIdentifier: cp}
		if partial {
			limit := 1
			request.LimitOverride = &limit
			request.ProviderConfig = json.RawMessage(`{"threshold":0}`)
		}
		payload, e := json.Marshal(struct {
			ExecutionID string          `json:"execution_id"`
			Request     d.SearchRequest `json:"request"`
		}{execution.String(), request})
		must(t, e)
		keyJSON, e := json.Marshal(struct {
			Kind                             string
			TenantID, ResourceID             uuid.UUID
			DomainAttempt, RedriveGeneration int
		}{"screening_execution", f.tenant, execution, 1, 0})
		must(t, e)
		keyHash := sha256.Sum256(keyJSON)
		payloadHash := sha256.Sum256(payload)
		input := mysqlpublication.IntakeRequest{Key: "decision-v1-" + hex.EncodeToString(keyHash[:]), PayloadSHA256: hex.EncodeToString(payloadHash[:]), TenantID: f.tenant, ExecutionID: execution, DomainAttempt: 1, Payload: payload}
		receipt, e := intake.Deliver(ctx, input)
		must(t, e)
		replay, e := intake.Deliver(ctx, input)
		must(t, e)
		if replay.ScreeningID != receipt.ScreeningID {
			t.Fatal("intake replay created a second source")
		}
		source, e := f.store.Screenings().GetByID(ctx, f.tenant.String(), receipt.ScreeningID.String())
		must(t, e)
		if cp != nil && (source.UniqueCounterpartyIdentifier == nil || *source.UniqueCounterpartyIdentifier != *cp) {
			t.Fatal("private intake dropped counterparty scope")
		}
		caseID := uuid.New()
		_, e = f.db.ExecContext(ctx, "INSERT INTO "+schema+"case_manager_cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES(?,?,?,'Synthetic screening journey','investigating','unset','decision',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(caseID), mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.inbox))
		must(t, e)
		_, e = f.db.ExecContext(ctx, "INSERT INTO "+schema+"case_manager_case_decisions(id,tenant_id,case_id,decision_id,scenario_id,object_type,object_id,pivot_hash,pivot_value,created_at) VALUES(?,?,?,?,?,'Person','synthetic-person',UNHEX(SHA2('synthetic-person',256)),'synthetic-person',UTC_TIMESTAMP(6))", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(f.tenant), mysqlstore.UUID(caseID), mysqlstore.UUID(decision), mysqlstore.UUID(scenario))
		must(t, e)
		must(t, dispatch.RunScreening(ctx, f.tenant.String(), source.ID))
		matches, e := f.store.ScreeningMatches().ListByScreening(ctx, f.tenant.String(), source.ID)
		must(t, e)
		for _, m := range matches {
			_, e = f.db.ExecContext(ctx, "INSERT INTO "+schema+"case_manager_case_screenings(id,tenant_id,case_id,screening_id,match_id,status,created_at) VALUES(?,?,?,?,?,'pending',UTC_TIMESTAMP(6))", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(f.tenant), mysqlstore.UUID(caseID), mysqlstore.UUID(uuid.MustParse(source.ID)), mysqlstore.UUID(uuid.MustParse(m.ID)))
			must(t, e)
		}
		source, e = f.store.Screenings().GetByID(ctx, f.tenant.String(), source.ID)
		must(t, e)
		return source, caseID, decision
	}
	review := func(caseID uuid.UUID, match string, cmd *identity.Command) (identity.State, error) {
		return apiStore.Review(ctx, p, f.tenant, caseID, match, cmd, func(ctx context.Context, s *native.Store, c *identity.Command) (identity.State, error) {
			return svc.InvestigatorReview(ctx, ports.MutationStore(s), f.tenant.String(), match, f.user.String(), c)
		})
	}
	cp := "account-001"
	source, caseID, decision := create(false, &cp)
	state, e := reviewer.Review(ctx, caseID, decision, nil)
	must(t, e)
	if len(state.Actions) != 1 || state.DisabledActions["approve"] == "" {
		t.Fatal("unresolved screening allowed approval", state)
	}
	_, e = reviewer.Review(ctx, caseID, decision, &identity.Command{OperationID: uuid.New(), ResourceID: decision.String(), ExpectedVersion: state.Version, Action: "approve", Findings: "Must be denied"})
	if e == nil {
		t.Fatal("forged approval accepted")
	}
	matches, e := f.store.ScreeningMatches().ListByScreening(ctx, f.tenant.String(), source.ID)
	must(t, e)
	before, e := review(caseID, matches[0].ID, nil)
	must(t, e)
	command := identity.Command{OperationID: uuid.New(), ResourceID: matches[0].ID, ExpectedVersion: before.Version, Action: "dismiss_and_whitelist", Findings: "Investigated distinct counterparty"}
	_, e = review(caseID, matches[0].ID, &command)
	must(t, e)
	_, e = review(caseID, matches[0].ID, &command)
	must(t, e)
	request := d.SearchRequest{Provider: "opensanctions", ForcedOutcome: "review", DecisionID: decision.String(), ScreeningConfigID: uuid.NewString(), IdempotencyKey: uuid.NewString(), Queries: []d.SearchQuery{{Name: "Alex Example", Type: "Person"}}, UniqueCounterpartyIdentifier: &cp}
	raw, _ := json.Marshal(request)
	captured, _, _, e := localmatching.Evaluate(ctx, raw)
	must(t, e)
	_, count, _, e := mysqlpublication.ApplyInlineCounterpartyExclusions(ctx, f.db, f.database, f.tenant, raw, captured)
	must(t, e)
	if count != 0 {
		t.Fatal("counterparty exclusion did not apply")
	}
	_, count, _, e = mysqlpublication.ApplyInlineCounterpartyExclusions(ctx, f.db, f.database, f.other, raw, captured)
	must(t, e)
	if count != 1 {
		t.Fatal("cross-tenant exclusion leaked")
	}
	otherCP := "account-002"
	request.UniqueCounterpartyIdentifier = &otherCP
	raw, _ = json.Marshal(request)
	captured, _, _, e = localmatching.Evaluate(ctx, raw)
	must(t, e)
	_, count, _, e = mysqlpublication.ApplyInlineCounterpartyExclusions(ctx, f.db, f.database, f.tenant, raw, captured)
	must(t, e)
	if count != 1 {
		t.Fatal("counterparty exclusion leaked to another account")
	}
	state, e = reviewer.Review(ctx, caseID, decision, nil)
	must(t, e)
	if state.DisabledActions["approve"] != "" {
		t.Fatal("dismissal did not unlock approval", state)
	}
	suppressed, _, _ := create(false, &cp)
	if suppressed.Status != d.StatusNoHit {
		t.Fatal("worker did not apply counterparty whitelist", suppressed.Status)
	}
	var applications int
	must(t, f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"screening_counterparty_applications WHERE tenant_id=? AND screening_id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(suppressed.ID))).Scan(&applications))
	if applications != 1 {
		t.Fatal("worker suppression lost its audit receipt", applications)
	}
	removalState, e := review(caseID, matches[0].ID, nil)
	must(t, e)
	removal := identity.Command{OperationID: uuid.New(), ResourceID: matches[0].ID, ExpectedVersion: removalState.Version, Action: "remove_counterparty_exclusion", Findings: "Reassess future matches"}
	_, e = review(caseID, matches[0].ID, &removal)
	must(t, e)
	_, e = review(caseID, matches[0].ID, &removal)
	must(t, e)
	request.UniqueCounterpartyIdentifier = &cp
	raw, _ = json.Marshal(request)
	captured, _, _, e = localmatching.Evaluate(ctx, raw)
	must(t, e)
	_, count, _, e = mysqlpublication.ApplyInlineCounterpartyExclusions(ctx, f.db, f.database, f.tenant, raw, captured)
	must(t, e)
	if count != 1 {
		t.Fatal("removal did not restore future matches")
	}
	var auditEvents int
	must(t, f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"screening_counterparty_exclusion_events WHERE tenant_id=?", mysqlstore.UUID(f.tenant)).Scan(&auditEvents))
	if auditEvents != 2 {
		t.Fatal("counterparty changes were not audited exactly once", auditEvents)
	}

	partial, case2, decision2 := create(true, &otherCP)
	matches, e = f.store.ScreeningMatches().ListByScreening(ctx, f.tenant.String(), partial.ID)
	must(t, e)
	before, e = review(case2, matches[0].ID, nil)
	must(t, e)
	if before.Eligible || !strings.Contains(before.Reason, "incomplete") {
		t.Fatal("partial match was reviewable", before)
	}
	if _, e = review(case2, matches[0].ID, &identity.Command{OperationID: uuid.New(), ResourceID: matches[0].ID, ExpectedVersion: before.Version, Action: "no_hit", Findings: "Must reject incomplete evidence"}); e == nil {
		t.Fatal("partial match mutation accepted")
	}
	blocked, e := reviewer.Review(ctx, case2, decision2, nil)
	must(t, e)
	if !strings.Contains(blocked.DisabledActions["approve"], "incomplete") {
		t.Fatal("partial screening did not block approval", blocked)
	}
	context, e := apiStore.RefinementContext(ctx, p, f.tenant, case2, uuid.MustParse(partial.ID))
	must(t, e)
	if !context.CanRefine {
		t.Fatal(context)
	}
	partialInput := f.manualInput()
	partialInput.Name = "Alex Example"
	partialInput.Threshold = 0
	partialInput.Limit = 1
	incomplete, e := svc.CreateManualSearch(native.WithPrincipal(ctx, p), f.tenant.String(), f.user.String(), partialInput)
	must(t, e)
	must(t, dispatch.RunScreening(ctx, f.tenant.String(), incomplete.ID))
	if _, e = apiStore.ApplyRefinement(ctx, p, f.tenant, case2, uuid.MustParse(partial.ID), native.RefinementCommand{OperationID: uuid.New(), ReplacementID: uuid.MustParse(incomplete.ID), ExpectedVersion: context.Version, Findings: "Must reject incomplete preview"}); e == nil {
		t.Fatal("incomplete preview was applied")
	}
	input := f.manualInput()
	input.Name = "Unrelated synthetic result"
	input.Threshold = 100
	preview, e := svc.CreateManualSearch(native.WithPrincipal(ctx, p), f.tenant.String(), f.user.String(), input)
	must(t, e)
	must(t, dispatch.RunScreening(ctx, f.tenant.String(), preview.ID))
	command2 := native.RefinementCommand{OperationID: uuid.New(), ReplacementID: uuid.MustParse(preview.ID), ExpectedVersion: context.Version, Findings: "Confirmed full beneficiary name with customer"}
	otherLink := uuid.New()
	_, e = f.db.ExecContext(ctx, "INSERT INTO "+schema+"case_manager_case_screenings(id,tenant_id,case_id,screening_id,status,created_at) VALUES(?,?,?,?,'no_hit',UTC_TIMESTAMP(6))", mysqlstore.UUID(otherLink), mysqlstore.UUID(f.tenant), mysqlstore.UUID(caseID), mysqlstore.UUID(uuid.MustParse(preview.ID)))
	must(t, e)
	if _, e = apiStore.ApplyRefinement(ctx, p, f.tenant, case2, uuid.MustParse(partial.ID), command2); e == nil {
		t.Fatal("preview already linked to another case was adopted")
	}
	_, e = f.db.ExecContext(ctx, "DELETE FROM "+schema+"case_manager_case_screenings WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(otherLink))
	must(t, e)
	stale := command2
	stale.ExpectedVersion = "stale"
	if _, e = apiStore.ApplyRefinement(ctx, p, f.tenant, case2, uuid.MustParse(partial.ID), stale); e == nil {
		t.Fatal("stale refinement accepted")
	}
	if _, e = apiStore.ApplyRefinement(ctx, p, f.other, case2, uuid.MustParse(partial.ID), command2); e == nil {
		t.Fatal("cross-tenant refinement accepted")
	}
	_, e = f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+"reject_refinement_history BEFORE INSERT ON "+schema+"case_manager_case_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture history failure'")
	must(t, e)
	if _, e = apiStore.ApplyRefinement(ctx, p, f.tenant, case2, uuid.MustParse(partial.ID), command2); e == nil {
		t.Fatal("history failure hidden")
	}
	var receipts int
	must(t, f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"screening_refinements").Scan(&receipts))
	if receipts != 0 {
		t.Fatal("refinement did not roll back")
	}
	_, e = f.db.ExecContext(ctx, "DROP TRIGGER "+schema+"reject_refinement_history")
	must(t, e)
	// Competing operators cannot fork a source, even with independent operation IDs.
	competitor := command2
	competitor.OperationID = uuid.New()
	type result struct {
		command native.RefinementCommand
		err     error
	}
	results := make(chan result, 2)
	for _, candidate := range []native.RefinementCommand{command2, competitor} {
		go func(command native.RefinementCommand) {
			_, err := apiStore.ApplyRefinement(ctx, p, f.tenant, case2, uuid.MustParse(partial.ID), command)
			results <- result{command, err}
		}(candidate)
	}
	wins := 0
	for range 2 {
		out := <-results
		if out.err == nil {
			wins++
			command2 = out.command
		}
	}
	if wins != 1 {
		t.Fatal("concurrent refinements must have one winner", wins)
	}
	_, e = apiStore.ApplyRefinement(ctx, p, f.tenant, case2, uuid.MustParse(partial.ID), command2)
	must(t, e)
	manual, e := apiStore.Screenings().GetByID(ctx, f.tenant.String(), preview.ID)
	must(t, e)
	scope, e := apiStore.CounterpartyScope(ctx, manual)
	must(t, e)
	if scope.IsManual || scope.DecisionID != decision2.String() || scope.UniqueCounterpartyIdentifier == nil || *scope.UniqueCounterpartyIdentifier != otherCP {
		t.Fatal("refined evidence lost original counterparty scope")
	}
	old, e := f.store.Screenings().GetByID(ctx, f.tenant.String(), partial.ID)
	must(t, e)
	if old.Status != partial.Status || old.Partial != partial.Partial {
		t.Fatal("original screening overwritten")
	}
	state, e = reviewer.Review(ctx, case2, decision2, nil)
	must(t, e)
	if state.DisabledActions["approve"] != "" {
		t.Fatal("complete refined no-hit did not unlock approval", state)
	}
	_, e = reviewer.Review(ctx, case2, decision2, &identity.Command{OperationID: uuid.New(), ResourceID: decision2.String(), ExpectedVersion: state.Version, Action: "approve", Findings: "Reviewed complete refined evidence"})
	must(t, e)
}
