package localdelivery

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlworkflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/screeningreceipt"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	owner "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlpublication"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Provider struct {
	store           *Store
	token           string
	cases           *mysqlworkflow.ReceiptReceiver
	screen          *mysqlpublication.Store
	intake          *screeningreceipt.Local
	effects         *owner.EffectStore
	projector       *mysqlpublication.Projector
	rejectCallbacks bool
}

// SetCallbackPolicy configures the development receiver before serving. A
// rejected delivery is retained as a real negative receipt, never a success.
func (p *Provider) SetCallbackPolicy(reject bool) { p.rejectCallbacks = reject }

func NewProvider(db *sql.DB, database, token string, tenant uuid.UUID, authorize Authorize) (*Provider, error) {
	if len(token) != 64 {
		return nil, ErrRequest
	}
	s, err := NewStore(db, database, tenant, authorize)
	if err != nil {
		return nil, err
	}
	cases, err := mysqlworkflow.NewReceiptReceiver(db, database, tenant)
	if err != nil {
		return nil, err
	}
	screen, err := mysqlpublication.New(db, database, tenant, mysqlpublication.Authorize(authorize))
	if err != nil {
		return nil, err
	}
	// Legacy exact-watchlist work stays local-development. Authored synthetic
	// catalog work uses the opensanctions alias and is processed by the native
	// screening worker, including captured synchronous evidence.
	intake, err := mysqlpublication.NewIntake(screen, []string{"local-development", "opensanctions"})
	if err != nil {
		return nil, err
	}
	adapter, err := screeningreceipt.New(intake)
	if err != nil {
		return nil, err
	}
	source, err := owner.NewScreeningSourceContract(database, 4<<20)
	if err != nil {
		return nil, err
	}
	effects, err := owner.NewEffectStore(db, database, tenant, 500, 4<<20, owner.PlatformAuthorizer(authorize), &source)
	if err != nil {
		return nil, err
	}
	sink, err := owner.NewScreeningProjectionSink(effects)
	if err != nil {
		return nil, err
	}
	projector, err := mysqlpublication.NewProjector(screen, database, sink)
	if err != nil {
		return nil, err
	}
	return &Provider{store: s, token: token, cases: cases, screen: screen, intake: adapter, effects: effects, projector: projector}, nil
}
func (p *Provider) Handler() http.Handler {
	return providerHandler(p.token, p.store.db, func(r ports.DispatchRequest) (*Provider, error) {
		if Validate(r, p.store.tenant) != nil {
			return nil, ErrRequest
		}
		return p, nil
	})
}
func providerHandler(credential string, db *sql.DB, resolve func(ports.DispatchRequest) (*Provider, error)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"ready","profile":"local-development-providers"}`)
	})
	for _, op := range []string{"deliver", "reconcile"} {
		mux.HandleFunc("POST /internal/local-deliveries/"+op, func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			token := strings.TrimPrefix(header, "Bearer ")
			if !strings.HasPrefix(header, "Bearer ") || subtle.ConstantTimeCompare([]byte(token), []byte(credential)) != 1 {
				http.Error(w, "authentication required", 401)
				return
			}
			var request ports.DispatchRequest
			d := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxPayloadBytes+(1<<20)))
			d.DisallowUnknownFields()
			if d.Decode(&request) != nil {
				http.Error(w, "invalid delivery", 400)
				return
			}
			var tail any
			if d.Decode(&tail) != io.EOF {
				http.Error(w, "invalid delivery", 400)
				return
			}
			p, err := resolve(request)
			if err != nil {
				http.Error(w, "invalid delivery tenant", 400)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			out, err := p.receive(ctx, request, op == "reconcile")
			if errors.Is(err, ErrRequest) && op == "deliver" {
				out, err = p.store.Reject(ctx, request)
			}
			if err != nil {
				slog.Error("local provider request failed", "kind", request.Kind, "error", err)
				http.Error(w, "local provider unavailable", 503)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(out)
		})
	}
	return mux
}
func (p *Provider) receive(ctx context.Context, r ports.DispatchRequest, reconcile bool) (ports.DispatchReceipt, error) {
	prior, err := p.store.Reconcile(ctx, r)
	if err != nil || prior.Outcome != ports.DispatchNotFound {
		return prior, err
	}
	if r.Kind == ports.JobScreening {
		var out ports.DispatchReceipt
		var err error
		if reconcile {
			out, err = p.intake.Reconcile(ctx, r)
		} else {
			out, err = p.intake.Deliver(ctx, r)
		}
		if errors.Is(err, mysqlpublication.ErrInvalid) {
			return out, ErrRequest
		}
		if err != nil || out.Outcome != ports.DispatchAccepted {
			return out, err
		}
		marker, _ := json.Marshal(map[string]any{"backend": "local-development-screening", "screening_id": out.Reference})
		return p.store.record(ctx, r, marker, ports.DispatchAccepted, uuid.MustParse(out.Reference))
	}
	if r.Kind == ports.JobWorkflow {
		var body struct {
			ActionType workflow.ActionType `json:"action_type"`
			Config     struct {
				URL string `json:"url"`
			} `json:"action_config"`
		}
		if json.Unmarshal(r.Payload, &body) != nil {
			return ports.DispatchReceipt{}, ErrRequest
		}
		if workflow.IsCaseAction(body.ActionType) {
			raw, _ := json.Marshal(r)
			value, err := p.cases.Receive(ctx, raw, reconcile)
			if err != nil {
				return ports.DispatchReceipt{}, err
			}
			var out ports.DispatchReceipt
			err = json.Unmarshal(value, &out)
			if err != nil || out.Outcome != ports.DispatchAccepted {
				return out, err
			}
			marker, _ := json.Marshal(map[string]any{"backend": "case-owner", "case_id": out.Reference})
			return p.store.record(ctx, r, marker, ports.DispatchAccepted, uuid.MustParse(out.Reference))
		}
		if body.ActionType != workflow.ActionTypeEmitEvent || body.Config.URL != EventURL {
			return ports.DispatchReceipt{}, ErrRequest
		}
	}
	if r.Kind == ports.JobCallback {
		if p.rejectCallbacks {
			return ports.DispatchReceipt{}, ErrRequest
		}
		var body struct {
			URL string `json:"callback_url"`
		}
		if json.Unmarshal(r.Payload, &body) != nil || body.URL != CallbackURL {
			return ports.DispatchReceipt{}, ErrRequest
		}
	}
	if r.Kind != ports.JobScoring && r.Kind != ports.JobOutbox && r.Kind != ports.JobWorkflow && r.Kind != ports.JobCallback {
		return ports.DispatchReceipt{}, ErrRequest
	}
	if reconcile {
		return p.store.Reconcile(ctx, r)
	}
	result := json.RawMessage(`{"backend":"local-development-inbox","received":true}`)
	if r.Kind == ports.JobScoring {
		var err error
		result, err = p.score(ctx, r)
		if err != nil {
			return ports.DispatchReceipt{}, err
		}
	}
	return p.store.Record(ctx, r, result)
}
func (p *Provider) score(ctx context.Context, r ports.DispatchRequest) (json.RawMessage, error) {
	var body struct {
		RequestID string `json:"request_id"`
		Request   struct {
			DecisionID string `json:"decision_id"`
			Ruleset    string `json:"ruleset_ref"`
		} `json:"request"`
	}
	if json.Unmarshal(r.Payload, &body) != nil || body.RequestID != r.ResourceID || body.Request.Ruleset != "local:decision-score-v1" {
		return nil, ErrRequest
	}
	id, err := uuid.Parse(body.Request.DecisionID)
	if err != nil {
		return nil, ErrRequest
	}
	var score int
	err = mysqlstore.Snapshot(ctx, p.store.db, func(tx *sql.Tx) error {
		if err := p.store.authorize(ctx, tx, p.store.tenant, false); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, "SELECT d.score FROM "+p.store.schema+".decision_engine_decisions d JOIN "+p.store.schema+".decision_engine_scoring_requests r ON r.tenant_id=d.tenant_id AND r.decision_id=d.id WHERE d.tenant_id=? AND d.id=? AND r.id=?", mysqlstore.UUID(p.store.tenant), mysqlstore.UUID(id), mysqlstore.UUID(uuid.MustParse(r.ResourceID))).Scan(&score)
	})
	if err != nil {
		return nil, err
	}
	band := "low"
	if score >= 10 {
		band = "medium"
	}
	if score >= 30 {
		band = "high"
	}
	return json.Marshal(map[string]any{"backend": "local-development-scoring", "ruleset_ref": body.Request.Ruleset, "score": score, "risk_band": band})
}
func (p *Provider) Run(ctx context.Context) error {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-timer.C:
			tick, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := p.ProcessOnce(tick)
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("local provider processing failed", "error", err)
			}
		}
	}
}

func (p *Provider) ProcessOnce(ctx context.Context) error {
	err := p.screen.ProcessLocalBatch(ctx, 20)
	if err == nil {
		_, err = p.projector.RunBatch(ctx, 20)
	}
	if err == nil {
		err = p.processScores(ctx)
	}
	return err
}
