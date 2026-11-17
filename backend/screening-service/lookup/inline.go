// Package lookup is screening's in-process read facade for the decision owner.
// It has no HTTP listener, source mutation, collector or customer-selected URL.
package lookup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUnavailable = errors.New("inline_screening_unavailable")
var ErrAuthority = errors.New("inline_screening_configuration_changed")

type Policy struct {
	Version         int    `json:"version"`
	Failure         string `json:"failure"`
	TimeoutMS       int64  `json:"timeout_ms"`
	ForcedOutcome   string `json:"forced_outcome"`
	OriginalOutcome string `json:"original_outcome"`
	FinalOutcome    string `json:"final_outcome"`
}
type Input struct {
	TenantID, ScenarioID, ConfigID, DecisionID, ExecutionID string
	Config, Request                                         json.RawMessage
	Policy                                                  Policy
}
type Result struct {
	Status     string
	MatchCount int
	Partial    bool
	Reference  string
	Policy     Policy
	input      Input
	request    screening.SearchRequest
	provider   screening.ProviderResult
	at         time.Time
	exclusions map[string]string
}
type Facade struct {
	DB        *pgxpool.Pool
	Transport *Transport
}

const authoritySQL = `SELECT EXISTS(SELECT 1 FROM core.screening_configs c JOIN core.scenarios sc ON(sc.tenant_id,sc.id)=(c.tenant_id,c.scenario_id) JOIN core.tenants t ON t.id=c.tenant_id WHERE c.tenant_id=$1 AND c.scenario_id=$2 AND c.id=$3 AND c.active AND c.provider='internal-watchlists' AND sc.live_iteration_id IS NOT NULL AND t.status='active' AND c.config_json=$4::jsonb)`

func (f Facade) Evaluate(ctx context.Context, in Input) (*Result, error) {
	out := &Result{Status: "failed", Policy: in.Policy, input: in, at: time.Now().UTC()}
	if json.Unmarshal(in.Request, &out.request) != nil {
		return out, ErrAuthority
	}
	if f.DB == nil || len(in.Request) > 16384 || len(in.Config) > 16384 {
		return out, ErrUnavailable
	}
	for _, id := range []string{in.TenantID, in.ScenarioID, in.ConfigID, in.DecisionID, in.ExecutionID} {
		if v, e := uuid.Parse(id); e != nil || v == uuid.Nil {
			return out, ErrAuthority
		}
	}
	if in.Policy.Version != 1 || in.Policy.Failure != "error" || in.Policy.TimeoutMS < 10 || in.Policy.TimeoutMS > 5000 {
		return out, ErrAuthority
	}
	var allowed bool
	bounded, cancel := context.WithTimeout(ctx, time.Duration(in.Policy.TimeoutMS)*time.Millisecond)
	defer cancel()
	ctx = bounded
	if e := f.DB.QueryRow(ctx, authoritySQL, in.TenantID, in.ScenarioID, in.ConfigID, in.Config).Scan(&allowed); e != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			out.Status = "timed_out"
		}
		return out, ErrUnavailable
	}
	if !allowed {
		return out, ErrAuthority
	}
	dec := json.NewDecoder(bytes.NewReader(in.Request))
	dec.DisallowUnknownFields()
	if dec.Decode(&out.request) != nil || dec.Decode(new(any)) != io.EOF {
		return out, ErrAuthority
	}
	var config struct {
		Provider json.RawMessage `json:"provider_config"`
		Forced   string          `json:"forced_outcome"`
	}
	if json.Unmarshal(in.Config, &config) != nil || config.Forced != in.Policy.ForcedOutcome || config.Forced == "" || out.request.Provider != sourcedata.Provider || out.request.DecisionID != in.DecisionID || out.request.ScenarioID != in.ScenarioID || out.request.ScreeningConfigID != in.ConfigID || out.request.IdempotencyKey != in.ExecutionID {
		return out, ErrAuthority
	}
	a, e := sourcedata.DecodeSelection(config.Provider)
	if e != nil {
		return out, ErrAuthority
	}
	b, e := sourcedata.DecodeSelection(out.request.ProviderConfig)
	if e != nil {
		return out, ErrAuthority
	}
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	if !bytes.Equal(left, right) {
		return out, ErrAuthority
	}
	permitted := []string{}
	for _, r := range a.Datasets {
		permitted = append(permitted, r.Scope+":"+r.ID)
	}
	scope := provideraccess.InlineConfig(uuid.MustParse(in.TenantID), uuid.MustParse(in.ConfigID), permitted)
	var engine ports.ScopedScreeningProvider = internalprovider.Provider{DB: f.DB}
	if f.Transport != nil {
		engine = f.Transport.client
	}
	result, e := engine.Search(bounded, scope, out.request)
	if e != nil {
		if errors.Is(bounded.Err(), context.DeadlineExceeded) {
			out.Status = "timed_out"
		}
		return out, ErrUnavailable
	}
	out.provider = result
	out.Reference = result.ProviderReference
	out.MatchCount = len(result.Matches)
	out.Partial = result.Partial
	queryBytes, _ := json.Marshal(out.request.Queries)
	fingerprint := sha256.Sum256(queryBytes)
	entities := make([]string, 0, len(result.Matches))
	for _, m := range result.Matches {
		entities = append(entities, m.EntityID)
	}
	out.exclusions = map[string]string{}
	rows, e := f.DB.Query(ctx, `SELECT entity_id,operation_id::text FROM screening.object_exclusions WHERE tenant_id=$1 AND object_type=$2 AND object_id=$3 AND provider=$4 AND query_fingerprint=$5 AND active AND entity_id=ANY($6::text[])`, in.TenantID, out.request.ObjectType, out.request.ObjectID, sourcedata.Provider, hex.EncodeToString(fingerprint[:]), entities)
	if e != nil {
		out.Status = "failed"
		return out, ErrUnavailable
	}
	for rows.Next() {
		var entity, operation string
		if e = rows.Scan(&entity, &operation); e != nil {
			rows.Close()
			return out, ErrUnavailable
		}
		out.exclusions[entity] = operation
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, ErrUnavailable
	}
	out.MatchCount -= len(out.exclusions)
	var evidence map[string]any
	if json.Unmarshal(result.RawResponse, &evidence) != nil {
		return out, ErrUnavailable
	}
	evidence["applied_exclusions"] = out.exclusions
	out.provider.RawResponse, _ = json.Marshal(evidence)
	out.Status = "no_candidates"
	if out.MatchCount > 0 {
		out.Status = "candidates"
	}
	if out.Partial {
		out.Status = "incomplete"
	}
	return out, nil
}

// Persist runs only inside the decision owner's transaction, after its decision
// and execution inserts. Source-owned writes, review routing and evidence either
// commit with that decision or all roll back. No provider/network IO occurs here.
func (f Facade) Persist(ctx context.Context, tx pgx.Tx, r *Result) error {
	if tx == nil || r == nil || r.at.IsZero() {
		return ErrAuthority
	}
	if r.Policy.Failure != "error" || r.Partial || (r.Status != "no_candidates" && r.Status != "candidates") {
		return ErrAuthority
	}
	in := r.input
	var locked uuid.UUID
	if e := tx.QueryRow(ctx, `SELECT id FROM core.screening_configs WHERE tenant_id=$1 AND id=$2 FOR SHARE`, in.TenantID, in.ConfigID).Scan(&locked); e != nil {
		return e
	}
	var allowed bool
	if e := tx.QueryRow(ctx, authoritySQL, in.TenantID, in.ScenarioID, in.ConfigID, in.Config).Scan(&allowed); e != nil {
		return e
	}
	if !allowed {
		return ErrAuthority
	}
	var cfg struct {
		Provider struct {
			Datasets []sourcedata.DatasetRef `json:"dataset_selection"`
		} `json:"provider_config"`
	}
	if json.Unmarshal(in.Config, &cfg) != nil {
		return ErrAuthority
	}
	// Current grant/tenant checks close revocation races between read and commit.
	refs, _ := json.Marshal(cfg.Provider.Datasets)
	var count int
	rows, e := tx.Query(ctx, `SELECT g.dataset_id FROM jsonb_to_recordset($2::jsonb) ref(scope text,id text) JOIN screening.source_grants g ON g.tenant_id=$1 AND g.dataset_id=ref.id AND g.owner_scope=CASE WHEN ref.scope='global' THEN '00000000-0000-0000-0000-000000000000'::uuid ELSE $1::uuid END ORDER BY g.owner_scope,g.dataset_id FOR SHARE OF g`, in.TenantID, refs)
	if e != nil {
		return e
	}
	for rows.Next() {
		count++
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if count != len(cfg.Provider.Datasets) {
		return ErrAuthority
	}
	source := screening.Screening{ID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("inline-screening/v1/"+in.TenantID+"/"+in.ExecutionID)).String(), TenantID: in.TenantID, DecisionID: in.DecisionID, ScenarioID: in.ScenarioID, ScreeningConfigID: in.ConfigID, IdempotencyKey: in.ExecutionID, Provider: sourcedata.Provider, ObjectType: r.request.ObjectType, ObjectID: r.request.ObjectID, RequestJSON: in.Request, ResponseJSON: r.provider.RawResponse, ProviderReference: r.Reference, Partial: r.Partial, UniqueCounterpartyIdentifier: r.request.UniqueCounterpartyIdentifier, CreatedAt: r.at, UpdatedAt: r.at, SentAt: &r.at, CompletedAt: &r.at, Status: screening.StatusNoHit}
	if r.MatchCount > 0 || r.Partial {
		source.Status = screening.StatusAwaitingReview
	}
	if r.Status == "failed" || r.Status == "timed_out" {
		source.Status = screening.StatusFailed
		source.CompletedAt = nil
		source.FailedAt = &r.at
		source.LastError = "inline_" + r.Status
	}
	if len(source.ResponseJSON) == 0 {
		source.ResponseJSON = json.RawMessage(`{}`)
	}
	if _, e := store.NewScreeningRepository(tx).Create(ctx, source); e != nil {
		return e
	}
	matches := make([]screening.Match, 0, r.MatchCount)
	for _, m := range r.provider.Matches {
		matches = append(matches, screening.Match{ID: m.EntityID + "-" + source.ID, TenantID: in.TenantID, ScreeningID: source.ID, EntityID: m.EntityID, Provider: sourcedata.Provider, Status: screening.MatchStatusPending, Name: m.Name, Score: m.Score, Payload: m.Payload, MatchedTexts: m.MatchedTexts, UniqueCounterpartyIdentifier: source.UniqueCounterpartyIdentifier, CreatedAt: r.at, UpdatedAt: r.at})
	}
	applications := map[string]string{}
	for i := range matches {
		if operation := r.exclusions[matches[i].EntityID]; operation != "" {
			matches[i].Status = screening.MatchStatusSkipped
			applications[matches[i].ID] = operation
		}
	}
	if e := store.NewScreeningMatchRepository(tx).ReplaceForScreening(ctx, source.ID, matches); e != nil {
		return e
	}
	if e := store.RepositoriesInTransaction(tx).ObjectExclusions().RecordApplications(ctx, in.TenantID, source.ID, applications); e != nil {
		return e
	}
	policy, _ := json.Marshal(r.Policy)
	if _, e := tx.Exec(ctx, `INSERT INTO screening.inline_evidence(tenant_id,decision_id,execution_id,screening_id,config_id,scenario_id,config_snapshot,request_snapshot,provider_evidence,policy_snapshot,lookup_status,evaluated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, in.TenantID, in.DecisionID, in.ExecutionID, source.ID, in.ConfigID, in.ScenarioID, in.Config, in.Request, source.ResponseJSON, policy, r.Status, r.at); e != nil {
		return e
	}
	if source.Status == screening.StatusAwaitingReview || source.Status == screening.StatusFailed {
		eventID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("screening.requires-review/v1/"+in.TenantID+"/"+source.ID)).String()
		if e := store.NewCaseEventRepository(tx).Enqueue(ctx, eventID, in.TenantID, "requires-review", ports.ScreeningRequiresReviewCommand{Version: 1, EventID: eventID, TenantID: in.TenantID, ScreeningID: source.ID}); e != nil {
			return e
		}
	}
	// Mark core execution source-managed immediately; callbacks cannot replace the
	// synchronous decision-time evidence or enqueue a fresh historical search.
	_, e = tx.Exec(ctx, `SELECT core.reconcile_screening_source($1,$2,$3,$4)`, in.TenantID, source.ID, in.DecisionID, in.ConfigID)
	return e
}
