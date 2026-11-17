// Package providerregistry composes named providers for both API and worker.
package providerregistry

import (
	"context"
	"encoding/json"
	"errors"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	legacy "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerproduct"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrProvider = errors.New("unknown_provider")

type Registry struct {
	Remote   *providerproduct.Client
	DB       *pgxpool.Pool
	Legacy   legacy.HTTPClient
	Internal internalprovider.Provider
}

func New(db *pgxpool.Pool, adapter legacy.HTTPClient) Registry {
	return Registry{DB: db, Legacy: adapter, Internal: internalprovider.Provider{DB: db}}
}
func (r Registry) scope(ctx context.Context) (provideraccess.Scope, error) {
	return provideraccess.FromContext(ctx)
}
func (r Registry) Prepare(ctx context.Context, tenant string, request screening.SearchRequest) (context.Context, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return ctx, err
	}
	if request.Provider != sourcedata.Provider || !s.Operator() {
		return ctx, nil
	}
	if !provideraccess.IsScenarioIntake(ctx) {
		return ctx, provideraccess.ErrScope
	}
	return r.scenarioScope(ctx, tenant, request)
}
func (r Registry) scenarioScope(ctx context.Context, tenant string, request screening.SearchRequest) (context.Context, error) {
	t, err := uuid.Parse(tenant)
	if err != nil || t == uuid.Nil {
		return ctx, provideraccess.ErrScope
	}
	id, err := uuid.Parse(request.IdempotencyKey)
	if err != nil || id == uuid.Nil || request.IsManual {
		return ctx, provideraccess.ErrScope
	}
	raw, _ := json.Marshal(request)
	var allowed bool
	err = r.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.screening_executions e JOIN core.screening_configs c ON (c.tenant_id,c.id,c.scenario_id)=(e.tenant_id,e.config_id,e.scenario_id) JOIN core.decisions d ON (d.tenant_id,d.id,d.scenario_id)=(e.tenant_id,e.decision_id,e.scenario_id) WHERE e.id=$1 AND e.tenant_id=$2 AND e.decision_id::text=$3 AND e.scenario_id::text=$4 AND e.config_id::text=$5 AND c.provider=$6 AND c.active AND e.request_json=$7::jsonb)`, id, t, request.DecisionID, request.ScenarioID, request.ScreeningConfigID, request.Provider, raw).Scan(&allowed)
	if err != nil || !allowed {
		return ctx, provideraccess.ErrScope
	}
	selection, err := sourcedata.DecodeSelection(request.ProviderConfig)
	if err != nil {
		return ctx, err
	}
	permitted := []string{}
	for _, ref := range selection.Datasets {
		permitted = append(permitted, ref.Scope+":"+ref.ID)
	}
	return provideraccess.WithScope(ctx, provideraccess.ScenarioExecution(t, id, permitted)), nil
}
func (r Registry) Validate(ctx context.Context, tenant string, request screening.SearchRequest) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}
	if !s.Operator() && s.Tenant().String() != tenant {
		return provideraccess.ErrScope
	}
	if request.Provider == sourcedata.Provider {
		return r.Internal.Validate(ctx, s, request)
	}
	if !r.Legacy.Configured(request.Provider) {
		return ErrProvider
	}
	return nil
}

// Capture is invoked inside the screening/queue transaction. Authority is kept
// separately from customer-controlled RequestJSON and is never returned in DTOs.
func (r Registry) Capture(ctx context.Context, tx pgx.Tx, item screening.Screening) error {
	if item.Provider != sourcedata.Provider {
		return nil
	}
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}
	if !s.Operator() && s.Tenant().String() != item.TenantID {
		return provideraccess.ErrScope
	}
	if s.Scenario() != uuid.Nil {
		raw, _ := json.Marshal(map[string]any{"kind": "scenario-execution", "execution_id": s.Scenario()})
		_, err = tx.Exec(ctx, `INSERT INTO screening.provider_run_authority(screening_id,tenant_id,authority) VALUES($1,$2,$3)`, item.ID, item.TenantID, raw)
		return err
	}
	if s.Continuous() != uuid.Nil {
		var snapshot json.RawMessage
		if err = tx.QueryRow(ctx, `SELECT c.field_map_json FROM screening.monitored_objects o JOIN screening.continuous_screening_configs c ON(c.tenant_id,c.id)=(o.tenant_id,o.config_id) WHERE o.tenant_id=$1 AND o.id=$2 AND c.enabled AND c.provider='internal-watchlists'`, item.TenantID, s.Continuous()).Scan(&snapshot); err != nil {
			return err
		}
		raw, _ := json.Marshal(map[string]any{"kind": "continuous-object", "object_id": s.Continuous(), "config_snapshot": snapshot})
		_, err = tx.Exec(ctx, `INSERT INTO screening.provider_run_authority(screening_id,tenant_id,authority) VALUES($1,$2,$3)`, item.ID, item.TenantID, raw)
		return err
	}
	if s.Operator() {
		return provideraccess.ErrScope
	} // controlled admin may publish, not impersonate a customer search.
	raw, err := json.Marshal(s.Principal())
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO screening.provider_run_authority(screening_id,tenant_id,authority) VALUES($1,$2,$3)`, item.ID, item.TenantID, raw)
	return err
}
func (r Registry) ForRun(ctx context.Context, item screening.Screening) (context.Context, error) {
	if item.Provider != sourcedata.Provider {
		return provideraccess.WithScope(ctx, provideraccess.GlobalOperator()), nil
	}
	var raw []byte
	if err := r.DB.QueryRow(ctx, `SELECT authority FROM screening.provider_run_authority WHERE screening_id=$1 AND tenant_id=$2`, item.ID, item.TenantID).Scan(&raw); err != nil {
		return ctx, provideraccess.ErrScope
	}
	var principal identity.Principal
	var authority struct {
		Kind      string    `json:"kind"`
		Execution uuid.UUID `json:"execution_id"`
		Target    uuid.UUID `json:"target_id"`
	}
	if json.Unmarshal(raw, &authority) == nil && authority.Kind == "continuous-object" {
		return r.continuousRun(ctx, item, raw)
	}
	if json.Unmarshal(raw, &authority) == nil && authority.Kind == "source-monitor" {
		var allowed bool
		tenant, e := uuid.Parse(item.TenantID)
		if e != nil || authority.Target == uuid.Nil {
			return ctx, provideraccess.ErrScope
		}
		if e = r.DB.QueryRow(ctx, provideraccess.MonitorAuthoritySQL, tenant, authority.Target).Scan(&allowed); e != nil || !allowed {
			return ctx, provideraccess.ErrScope
		}
		var related bool
		if e = r.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening.source_rescreen_targets WHERE id=$1 AND tenant_id=$2 AND screening_id=$3)`, authority.Target, tenant, item.ID).Scan(&related); e != nil || !related {
			return ctx, provideraccess.ErrScope
		}
		var request screening.SearchRequest
		if json.Unmarshal(item.RequestJSON, &request) != nil {
			return ctx, sourcedata.ErrInput
		}
		selection, e := sourcedata.DecodeSelection(request.ProviderConfig)
		if e != nil {
			return ctx, e
		}
		permitted := []string{}
		for _, ref := range selection.Datasets {
			permitted = append(permitted, ref.Scope+":"+ref.ID)
		}
		return provideraccess.WithScope(ctx, provideraccess.MonitorExecution(tenant, authority.Target, permitted)), nil
	}
	if json.Unmarshal(raw, &authority) == nil && authority.Kind == "scenario-execution" {
		var request screening.SearchRequest
		if json.Unmarshal(item.RequestJSON, &request) != nil || request.IdempotencyKey != authority.Execution.String() {
			return ctx, provideraccess.ErrScope
		}
		return r.scenarioScope(ctx, item.TenantID, request)
	}
	if json.Unmarshal(raw, &principal) != nil || principal.TenantID.String() != item.TenantID {
		return ctx, provideraccess.ErrScope
	}
	s, err := provideraccess.FromPrincipal(principal)
	if err != nil {
		return ctx, err
	}
	if err = identity.CheckIdentity(ctx, r.DB, principal, false); err != nil {
		return ctx, err
	}
	return provideraccess.WithScope(ctx, s), nil
}
func (r Registry) Search(ctx context.Context, request screening.SearchRequest) (screening.ProviderResult, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return screening.ProviderResult{}, err
	}
	if request.Provider == sourcedata.Provider {
		if r.Remote != nil {
			return r.Remote.Search(ctx, s, request)
		}
		return r.Internal.Search(ctx, s, request)
	}
	if !r.Legacy.Configured(request.Provider) {
		return screening.ProviderResult{}, ErrProvider
	}
	return r.Legacy.Search(ctx, request)
}

type evidenceKey struct{}

func (r Registry) ForMatch(ctx context.Context, match screening.Match) (context.Context, error) {
	if match.Provider != sourcedata.Provider {
		return ctx, nil
	}
	var payload struct {
		Provenance sourcedata.CandidateRef `json:"provenance"`
	}
	if json.Unmarshal(match.Payload, &payload) != nil || payload.Provenance.RecordID == "" {
		return ctx, sourcedata.ErrEvidence
	}
	s, err := r.scope(ctx)
	if err != nil || s.Operator() || s.Tenant().String() != match.TenantID {
		return ctx, provideraccess.ErrScope
	}
	return context.WithValue(ctx, evidenceKey{}, payload.Provenance), nil
}
func (r Registry) Enrich(ctx context.Context, provider, entity string) (screening.EnrichmentResult, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return screening.EnrichmentResult{}, err
	}
	if provider == sourcedata.Provider {
		ref, ok := ctx.Value(evidenceKey{}).(sourcedata.CandidateRef)
		if !ok {
			return screening.EnrichmentResult{}, sourcedata.ErrEvidence
		}
		if r.Remote != nil {
			return r.Remote.Enrich(ctx, s, ref)
		}
		return r.Internal.Enrich(ctx, s, ref)
	}
	if !r.Legacy.Configured(provider) {
		return screening.EnrichmentResult{}, ErrProvider
	}
	return r.Legacy.Enrich(ctx, provider, entity)
}
func (r Registry) GetCatalog(ctx context.Context, provider string) (screening.DatasetCatalog, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return screening.DatasetCatalog{}, err
	}
	if provider == sourcedata.Provider {
		if r.Remote != nil {
			return r.Remote.GetCatalog(ctx, s, sourcedata.Operation{Provider: provider})
		}
		return r.Internal.GetCatalog(ctx, s, sourcedata.Operation{Provider: provider})
	}
	if !r.Legacy.Configured(provider) {
		return screening.DatasetCatalog{}, ErrProvider
	}
	return r.Legacy.GetCatalog(ctx, provider)
}

type selectionKey struct{}

func WithSelection(ctx context.Context, refs []sourcedata.DatasetRef) context.Context {
	return context.WithValue(ctx, selectionKey{}, refs)
}
func (r Registry) GetFreshness(ctx context.Context, provider string) (screening.DatasetFreshness, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return screening.DatasetFreshness{}, err
	}
	if provider == sourcedata.Provider {
		refs, _ := ctx.Value(selectionKey{}).([]sourcedata.DatasetRef)
		if r.Remote != nil {
			return r.Remote.GetFreshness(ctx, s, sourcedata.Operation{Provider: provider, Selection: refs})
		}
		return r.Internal.GetFreshness(ctx, s, sourcedata.Operation{Provider: provider, Selection: refs})
	}
	if !r.Legacy.Configured(provider) {
		return screening.DatasetFreshness{}, ErrProvider
	}
	return r.Legacy.GetFreshness(ctx, provider)
}
func (r Registry) GetDatasetDelta(ctx context.Context, provider, cursor string) (screening.DatasetDelta, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return screening.DatasetDelta{}, err
	}
	if provider == sourcedata.Provider {
		refs, _ := ctx.Value(selectionKey{}).([]sourcedata.DatasetRef)
		if r.Remote != nil {
			return r.Remote.GetDatasetDelta(ctx, s, sourcedata.Operation{Provider: provider, Selection: refs, Cursor: cursor})
		}
		return r.Internal.GetDatasetDelta(ctx, s, sourcedata.Operation{Provider: provider, Selection: refs, Cursor: cursor})
	}
	if !r.Legacy.Configured(provider) {
		return screening.DatasetDelta{}, ErrProvider
	}
	return r.Legacy.GetDatasetDelta(ctx, provider, cursor)
}

type Capabilities struct {
	legacy.ManualCapabilities
	Datasets      []internalprovider.Dataset     `json:"datasets,omitempty"`
	Profiles      []string                       `json:"profiles,omitempty"`
	ProfileFields map[string]map[string][]string `json:"profile_fields,omitempty"`
}

func (r Registry) Capabilities(ctx context.Context) ([]Capabilities, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}
	items := []Capabilities{{ManualCapabilities: r.Legacy.ManualCapabilities()}}
	fields := map[string][]string{"Person": {"name"}, "Organization": {"name"}, "Vessel": {"name"}, "Aircraft": {"name"}}
	cap := legacy.ManualCapabilities{ID: sourcedata.Provider, Name: "Internal watchlists", Health: "unknown", EntityTypes: []string{"Person", "Organization", "Vessel", "Aircraft"}, Fields: fields, DatasetSelection: true, ThresholdMin: 0, ThresholdMax: 100, LimitMax: 50, ThresholdSemantics: "Versioned profile similarity, not identity confidence; fuzzy threshold minimum 50", ResultSemantics: "Published PostgreSQL source records; immutable generation evidence", Limitations: []string{"Only explicitly selected published datasets are searched; source rights/freshness must be approved. Scores require review; cross-script variants require explicit aliases."}}
	datasets, err := r.Internal.Visible(ctx, s)
	if err == nil && r.Remote != nil {
		var remote []internalprovider.Dataset
		remote, err = r.Remote.Visible(ctx, s)
		if err == nil {
			authorized := map[sourcedata.DatasetRef]bool{}
			for _, d := range remote {
				authorized[d.Ref] = true
			}
			filtered := datasets[:0]
			for _, d := range datasets {
				if authorized[d.Ref] {
					filtered = append(filtered, d)
				}
			}
			datasets = filtered
		}
	}
	if err != nil {
		if errors.Is(err, sourcedata.ErrNotReady) {
			items = append(items, Capabilities{ManualCapabilities: cap})
			return items, nil
		}
		return nil, err
	}
	for _, d := range datasets {
		if d.Generation != nil {
			cap.Configured = true
		}
	}
	profiles := []string{sourcedata.Profile}
	profileFields := map[string]map[string][]string{sourcedata.Profile: fields}
	var fuzzy bool
	if err = r.DB.QueryRow(ctx, `SELECT to_regclass('screening.source_names_fuzzy_lookup') IS NOT NULL`).Scan(&fuzzy); err != nil {
		return nil, err
	}
	if fuzzy {
		profiles = append(profiles, sourcedata.FuzzyProfile)
		profiles = append(profiles, sourcedata.TokenProfile)
		profiles = append(profiles, sourcedata.IdentifierProfile)
		profileFields[sourcedata.FuzzyProfile] = map[string][]string{"Person": {"name", "birth_date", "nationality", "address"}, "Organization": {"name", "country", "registration_number", "address"}, "Vessel": {"name", "address"}, "Aircraft": {"name", "address"}}
		profileFields[sourcedata.TokenProfile] = profileFields[sourcedata.FuzzyProfile]
		profileFields[sourcedata.IdentifierProfile] = map[string][]string{"Person": {"name", "birth_date", "nationality", "address", "identifiers"}, "Organization": {"name", "country", "registration_number", "address", "identifiers"}, "Vessel": {"name", "address", "identifiers"}, "Aircraft": {"name", "address", "identifiers"}}
	}
	items = append(items, Capabilities{ManualCapabilities: cap, Datasets: datasets, Profiles: profiles, ProfileFields: profileFields})
	return items, nil
}
