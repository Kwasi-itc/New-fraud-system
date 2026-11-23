package internalprovider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sort"
	"time"
)

type Provider struct{ DB *pgxpool.Pool }
type Dataset struct {
	Ref         sourcedata.DatasetRef `json:"ref"`
	Name        string                `json:"name"`
	Category    string                `json:"category"`
	Generation  *uuid.UUID            `json:"generation"`
	PublishedAt *time.Time            `json:"published_at"`
	Count       int                   `json:"record_count"`
	Normalizer  string                `json:"normalizer"`
	Source      *SourceReadiness      `json:"source,omitempty"`
	owner       uuid.UUID
	grant       uuid.UUID
}
type SourceReadiness struct {
	Connector         string     `json:"connector"`
	Rights            string     `json:"rights_state"`
	CollectionEnabled bool       `json:"collection_enabled"`
	LastSuccess       *time.Time `json:"last_success_at"`
	Ready             bool       `json:"ready"`
}

func (p Provider) authorize(ctx context.Context, s provideraccess.Scope) error {
	if p.DB == nil || !s.Valid() {
		return provideraccess.ErrScope
	}
	if !s.Operator() {
		if s.Client() != uuid.Nil {
			var topology string
			err := p.DB.QueryRow(ctx, `SELECT i.topology FROM provider_api.clients c JOIN provider_api.credentials k ON k.client_id=c.id CROSS JOIN provider_api.installation i WHERE i.singleton AND c.id=$1 AND c.namespace_id=$2 AND c.revision=$3 AND c.enabled AND c.expires_at>now() AND k.id=$4 AND NOT k.revoked AND k.expires_at>now()`, s.Client(), s.Tenant(), s.ClientRevision(), s.Credential()).Scan(&topology)
			if err != nil {
				return provideraccess.ErrScope
			}
			if topology == "shared" {
				var active bool
				if err = p.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.tenants WHERE id=$1 AND status='active')`, s.Tenant()).Scan(&active); err != nil || !active {
					return provideraccess.ErrScope
				}
			} else if topology != "standalone" {
				return provideraccess.ErrScope
			}
			return nil
		}
		if s.Continuous() != uuid.Nil {
			var allowed bool
			e := p.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening.monitored_objects o JOIN screening.continuous_screening_configs c ON(c.tenant_id,c.id)=(o.tenant_id,o.config_id) JOIN core.tenants t ON t.id=o.tenant_id WHERE o.tenant_id=$1 AND o.id=$2 AND c.provider='internal-watchlists' AND c.enabled AND c.object_type=o.object_type AND o.status IN('pending','active') AND t.status='active')`, s.Tenant(), s.Continuous()).Scan(&allowed)
			if e != nil || !allowed {
				return provideraccess.ErrScope
			}
			return nil
		}
		if s.Inline() != uuid.Nil {
			var allowed bool
			err := p.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.screening_configs c JOIN core.scenarios sc ON(sc.tenant_id,sc.id)=(c.tenant_id,c.scenario_id) JOIN core.tenants t ON t.id=c.tenant_id WHERE c.tenant_id=$1 AND c.id=$2 AND c.active AND c.provider='internal-watchlists' AND sc.live_iteration_id IS NOT NULL AND t.status='active')`, s.Tenant(), s.Inline()).Scan(&allowed)
			if err != nil || !allowed {
				return provideraccess.ErrScope
			}
			return nil
		}
		if s.Monitor() != uuid.Nil {
			var allowed bool
			if err := p.DB.QueryRow(ctx, provideraccess.MonitorAuthoritySQL, s.Tenant(), s.Monitor()).Scan(&allowed); err != nil || !allowed {
				return provideraccess.ErrScope
			}
			return nil
		}
		if s.Scenario() != uuid.Nil {
			var allowed bool
			err := p.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.screening_executions e JOIN core.screening_configs c ON (c.tenant_id,c.id,c.scenario_id)=(e.tenant_id,e.config_id,e.scenario_id) JOIN core.decisions d ON (d.tenant_id,d.id,d.scenario_id)=(e.tenant_id,e.decision_id,e.scenario_id) JOIN core.tenants t ON t.id=e.tenant_id WHERE e.tenant_id=$1 AND e.id=$2 AND c.active AND c.provider='internal-watchlists' AND t.status='active')`, s.Tenant(), s.Scenario()).Scan(&allowed)
			if err != nil || !allowed {
				return provideraccess.ErrScope
			}
			return nil
		}
		return identity.CheckIdentity(ctx, p.DB, s.Principal(), false)
	}
	return nil
}
func (p Provider) Visible(ctx context.Context, s provideraccess.Scope) ([]Dataset, error) {
	if err := p.authorize(ctx, s); err != nil {
		return nil, err
	}
	statement := `SELECT d.owner_scope,d.id,d.label,d.category,CASE WHEN v.status='published' AND v.normalizer=$3 AND (c.connector IS NULL OR $2 OR c.ready) THEN d.active_generation END,v.published_at,coalesce(v.record_count,0),coalesce(g.revision,'00000000-0000-0000-0000-000000000000'),coalesce(c.connector,''),coalesce(c.rights_state,''),coalesce(c.enabled,false),c.last_success_at,coalesce(c.ready,false),coalesce(v.normalizer,'')
 FROM screening.source_datasets d LEFT JOIN screening.source_versions v ON (v.owner_scope,v.dataset_id,v.id)=(d.owner_scope,d.id,d.active_generation)
 LEFT JOIN screening.source_readiness_v1 c ON (c.owner_scope,c.dataset_id)=(d.owner_scope,d.id)
 LEFT JOIN screening.source_grants g ON g.tenant_id=$1 AND (g.owner_scope,g.dataset_id)=(d.owner_scope,d.id)
 WHERE d.enabled AND (c.connector IS NULL OR $2 OR c.rights_state='approved') AND (($2 AND d.owner_scope='00000000-0000-0000-0000-000000000000') OR (NOT $2 AND g.tenant_id IS NOT NULL AND (d.owner_scope='00000000-0000-0000-0000-000000000000' OR d.owner_scope=$1)))
 ORDER BY d.owner_scope,d.id LIMIT 501`
	args := []any{s.Tenant(), s.Operator(), sourcedata.Normalizer}
	if s.Client() != uuid.Nil {
		statement = `SELECT d.owner_scope,d.id,d.label,d.category,CASE WHEN v.status='published' AND v.normalizer=$3 AND (c.connector IS NULL OR c.ready) THEN d.active_generation END,v.published_at,coalesce(v.record_count,0),cg.revision,coalesce(c.connector,''),coalesce(c.rights_state,''),coalesce(c.enabled,false),c.last_success_at,coalesce(c.ready,false),coalesce(v.normalizer,'')
 FROM provider_api.client_datasets cg JOIN screening.source_datasets d ON(d.owner_scope,d.id)=(cg.owner_scope,cg.dataset_id)
 CROSS JOIN provider_api.installation i LEFT JOIN screening.source_grants g ON g.tenant_id=$1 AND(g.owner_scope,g.dataset_id)=(d.owner_scope,d.id)
 LEFT JOIN screening.source_versions v ON(v.owner_scope,v.dataset_id,v.id)=(d.owner_scope,d.id,d.active_generation)
 LEFT JOIN screening.source_readiness_v1 c ON(c.owner_scope,c.dataset_id)=(d.owner_scope,d.id)
 WHERE i.singleton AND cg.client_id=$2 AND cg.namespace_id=$1 AND d.enabled AND(c.connector IS NULL OR c.rights_state='approved')
 AND(d.owner_scope='00000000-0000-0000-0000-000000000000' OR d.owner_scope=$1)
 AND(i.topology='standalone' OR(i.topology='shared' AND g.tenant_id IS NOT NULL)) ORDER BY d.owner_scope,d.id LIMIT 501`
		args = []any{s.Tenant(), s.Client(), sourcedata.Normalizer}
	}
	rows, err := p.DB.Query(ctx, statement, args...)
	if err != nil {
		return nil, sourcedata.ErrNotReady
	}
	defer rows.Close()
	items := []Dataset{}
	scanned := 0
	for rows.Next() {
		scanned++
		var d Dataset
		var source SourceReadiness
		if err = rows.Scan(&d.owner, &d.Ref.ID, &d.Name, &d.Category, &d.Generation, &d.PublishedAt, &d.Count, &d.grant, &source.Connector, &source.Rights, &source.CollectionEnabled, &source.LastSuccess, &source.Ready, &d.Normalizer); err != nil {
			return nil, err
		}
		if source.Connector != "" {
			d.Source = &source
		}
		d.Ref.Scope = "global"
		if d.owner != uuid.Nil {
			d.Ref.Scope = "tenant"
		}
		if !s.AllowsDataset(d.Ref.Scope, d.Ref.ID) {
			continue
		}
		items = append(items, d)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if scanned > 500 {
		return nil, sourcedata.ErrNotReady
	}
	return items, nil
}
func (p Provider) selected(ctx context.Context, s provideraccess.Scope, refs []sourcedata.DatasetRef, ready bool) ([]Dataset, error) {
	if len(refs) == 0 || len(refs) > sourcedata.MaxSelectedDatasets {
		return nil, sourcedata.ErrInput
	}
	items, err := p.Visible(ctx, s)
	if err != nil {
		return nil, err
	}
	index := map[sourcedata.DatasetRef]Dataset{}
	for _, d := range items {
		index[d.Ref] = d
	}
	seen := map[sourcedata.DatasetRef]bool{}
	out := make([]Dataset, 0, len(refs))
	for _, ref := range refs {
		if !ref.Valid() || seen[ref] {
			return nil, sourcedata.ErrInput
		}
		seen[ref] = true
		d, ok := index[ref]
		if !ok {
			return nil, sourcedata.ErrDataset
		}
		if ready && d.Generation == nil {
			return nil, sourcedata.ErrNotReady
		}
		out = append(out, d)
	}
	return out, nil
}
func (p Provider) Validate(ctx context.Context, s provideraccess.Scope, r screening.SearchRequest) error {
	_, _, err := p.prepare(ctx, s, r)
	return err
}
func (p Provider) prepare(ctx context.Context, s provideraccess.Scope, r screening.SearchRequest) (sourcedata.Selection, []Dataset, error) {
	if !sourcedata.NormalizerSupported() {
		return sourcedata.Selection{}, nil, sourcedata.ErrNotReady
	}
	v, err := sourcedata.DecodeSelection(r.ProviderConfig)
	if err != nil {
		return v, nil, err
	}
	if r.Provider != sourcedata.Provider || len(r.Queries) < 1 || len(r.Queries) > 10 || (r.LimitOverride != nil && (*r.LimitOverride < 1 || *r.LimitOverride > 50)) {
		return v, nil, sourcedata.ErrInput
	}
	for _, q := range r.Queries {
		if sourcedata.ValidateQueryIdentifiers(q.Identifiers) != nil || (len(q.Identifiers) > 0 && v.Profile != sourcedata.IdentifierProfile) {
			return v, nil, sourcedata.ErrInput
		}
		if !validFuzzyQuery(q) {
			return v, nil, sourcedata.ErrInput
		}
		if sourcedata.Normalize(q.Name) == "" || len(q.Name) > 800 || (q.Type != "Person" && q.Type != "Organization" && q.Type != "Vessel" && q.Type != "Aircraft") || (v.Profile == sourcedata.Profile && (q.BirthDate != "" || q.Nationality != "" || q.Country != "" || q.RegistrationNumber != "" || q.Address != "")) || len(q.RegistrationNumber) > 100 || len(q.BirthDate) > 10 || len(q.Address) > 1200 || len(q.Country) > 2 || len(q.Nationality) > 2 || (q.Type == "Person" && (q.Country != "" || q.RegistrationNumber != "")) || (q.Type == "Organization" && (q.BirthDate != "" || q.Nationality != "")) || ((q.Type == "Vessel" || q.Type == "Aircraft") && (q.BirthDate != "" || q.Nationality != "" || q.Country != "" || q.RegistrationNumber != "")) {
			return v, nil, sourcedata.ErrInput
		}
	}
	datasets, err := p.selected(ctx, s, v.Datasets, true)
	return v, datasets, err
}
func (p Provider) Search(ctx context.Context, s provideraccess.Scope, r screening.SearchRequest) (screening.ProviderResult, error) {
	v, datasets, err := p.prepare(ctx, s, r)
	if err != nil {
		return screening.ProviderResult{}, err
	}
	if sourcedata.IsFuzzyProfile(v.Profile) {
		return p.searchFuzzy(ctx, s, r, v, datasets)
	}
	limit := 10
	if r.LimitOverride != nil {
		limit = *r.LimitOverride
	}
	// Pin all selected generations once, then use a single batched indexed read.
	selection := []map[string]any{}
	for _, d := range datasets {
		selection = append(selection, map[string]any{"owner": d.owner, "dataset": d.Ref.ID, "generation": *d.Generation})
	}
	queries := []map[string]string{}
	for _, q := range r.Queries {
		queries = append(queries, map[string]string{"name": sourcedata.Normalize(q.Name), "type": q.Type})
	}
	sel, _ := json.Marshal(selection)
	qs, _ := json.Marshal(queries)
	rows, err := p.DB.Query(ctx, `SELECT r.owner_scope,r.dataset_id,r.generation,r.record_id,r.entity_id,r.record-'original_xml',n.original
 FROM jsonb_to_recordset($1::jsonb) AS d(owner uuid,dataset text,generation uuid)
 JOIN screening.source_names n ON (n.owner_scope,n.dataset_id,n.generation)=(d.owner,d.dataset,d.generation)
 JOIN screening.source_records r ON (r.owner_scope,r.dataset_id,r.generation,r.record_id)=(n.owner_scope,n.dataset_id,n.generation,n.record_id)
 WHERE EXISTS (SELECT 1 FROM jsonb_to_recordset($2::jsonb) AS q(name text,type text) WHERE q.name=n.normalized AND q.type=r.entity_type)
 ORDER BY r.entity_id,n.original LIMIT 1051`, sel, qs)
	if err != nil {
		return screening.ProviderResult{}, sourcedata.ErrNotReady
	}
	defer rows.Close()
	matches := []screening.ProviderMatch{}
	seen := map[uuid.UUID]bool{}
	scanned := 0
	for rows.Next() {
		var owner, generation, entity uuid.UUID
		var dataset, id, alias string
		var raw []byte
		if err = rows.Scan(&owner, &dataset, &generation, &id, &entity, &raw, &alias); err != nil {
			return screening.ProviderResult{}, err
		}
		scanned++
		if seen[entity] {
			continue
		}
		seen[entity] = true
		var record sourcedata.Record
		if json.Unmarshal(raw, &record) != nil {
			return screening.ProviderResult{}, sourcedata.ErrEvidence
		}
		ref := sourcedata.DatasetRef{Scope: "global", ID: dataset}
		if owner != uuid.Nil {
			ref.Scope = "tenant"
		}
		payload := candidatePayload(record, sourcedata.CandidateRef{Dataset: ref, Generation: generation, RecordID: id}, alias)
		matches = append(matches, screening.ProviderMatch{EntityID: entity.String(), Name: record.Name, Score: 1, Payload: payload, MatchedTexts: []string{alias}})
	}
	if err = rows.Err(); err != nil {
		return screening.ProviderResult{}, err
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].EntityID < matches[j].EntityID })
	partial := len(matches) > limit || scanned > 1050
	if len(matches) > limit {
		matches = matches[:limit]
	}
	encoded, _ := json.Marshal([]any{r, datasets})
	digest := sha256.Sum256(append([]byte(s.Key()), encoded...))
	reference := hex.EncodeToString(digest[:])
	response, _ := json.Marshal(map[string]any{"contract_version": 2, "provider": sourcedata.Provider, "profile": sourcedata.Profile, "datasets": datasets, "partial": partial, "matches": matches})
	return screening.ProviderResult{ProviderReference: reference, RawResponse: response, Partial: partial, Matches: matches}, nil
}
func candidatePayload(r sourcedata.Record, ref sourcedata.CandidateRef, alias string) json.RawMessage {
	props := map[string][]string{}
	for k, v := range r.Properties {
		props[k] = v
	}
	props["alias"] = r.Aliases
	props["source"] = []string{r.SourceReference}
	props["sourceRecord"] = []string{r.ID}
	props["datasetVersion"] = []string{ref.Generation.String()}
	props["matchingProfile"] = []string{sourcedata.Profile}
	props["matchedAlias"] = []string{alias}
	props["matchReason"] = []string{"Exact NFKC/case-folded name or alias; identity is not confirmed"}
	if r.SourceType != "" {
		props["sourceType"] = []string{r.SourceType}
	}
	if quality := r.AliasQualities[alias]; quality != "" {
		props["matchedAliasQuality"] = []string{quality}
	}
	for _, id := range r.Identifiers {
		label := id.Type
		if id.Country != "" {
			label += " / " + id.Country
		}
		if id.Issuer != "" {
			label += " / " + id.Issuer
		}
		props["sourceIdentifiers"] = append(props["sourceIdentifiers"], label+": "+id.Value)
	}
	raw, _ := json.Marshal(map[string]any{"schema": r.Type, "datasets": []string{ref.Dataset.ID}, "properties": props, "provenance": ref, "profile": sourcedata.Profile})
	return raw
}
func (p Provider) Enrich(ctx context.Context, s provideraccess.Scope, ref sourcedata.CandidateRef) (screening.EnrichmentResult, error) {
	if ref.Generation == uuid.Nil || ref.RecordID == "" {
		return screening.EnrichmentResult{}, sourcedata.ErrInput
	}
	ds, err := p.selected(ctx, s, []sourcedata.DatasetRef{ref.Dataset}, false)
	if err != nil {
		// Archiving a private list removes it from active discovery/search but
		// cannot erase published evidence. Recheck current identity and grant;
		// only the owning tenant may read a retained archived generation.
		if ref.Dataset.Scope != "tenant" || !s.AllowsDataset("tenant", ref.Dataset.ID) || p.authorize(ctx, s) != nil {
			return screening.EnrichmentResult{}, err
		}
		var allowed bool
		e := p.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening.private_lists l JOIN screening.source_grants g ON(g.tenant_id,g.owner_scope,g.dataset_id)=(l.tenant_id,l.tenant_id,l.dataset_id) WHERE l.tenant_id=$1 AND l.dataset_id=$2 AND l.archived)`, s.Tenant(), ref.Dataset.ID).Scan(&allowed)
		if e != nil || !allowed {
			return screening.EnrichmentResult{}, err
		}
		ds = []Dataset{{owner: s.Tenant()}}
	}
	var raw []byte
	var published bool
	err = p.DB.QueryRow(ctx, `SELECT r.record-'original_xml',v.status='published' FROM screening.source_records r JOIN screening.source_versions v ON (v.owner_scope,v.dataset_id,v.id)=(r.owner_scope,r.dataset_id,r.generation) WHERE r.owner_scope=$1 AND r.dataset_id=$2 AND r.generation=$3 AND r.record_id=$4`, ds[0].owner, ref.Dataset.ID, ref.Generation, ref.RecordID).Scan(&raw, &published)
	if err != nil || !published {
		return screening.EnrichmentResult{}, sourcedata.ErrEvidence
	}
	var record sourcedata.Record
	if json.Unmarshal(raw, &record) != nil {
		return screening.EnrichmentResult{}, sourcedata.ErrEvidence
	}
	record.OriginalXML = "" // raw source artifacts belong to the restricted ingestion boundary
	// Supplemental evidence is namespaced; never overwrite the original match.
	result, _ := json.Marshal(map[string]any{"supplement": map[string]any{"contract_version": 2, "provenance": ref, "source_reference": record.SourceReference, "record": record}})
	return screening.EnrichmentResult{RawPayload: result}, nil
}
func (p Provider) GetCatalog(ctx context.Context, s provideraccess.Scope, op sourcedata.Operation) (screening.DatasetCatalog, error) {
	if op.Provider != sourcedata.Provider || op.Limit < 0 || op.Limit > 500 {
		return screening.DatasetCatalog{}, sourcedata.ErrInput
	}
	var ds []Dataset
	var err error
	if len(op.Selection) > 0 {
		ds, err = p.selected(ctx, s, op.Selection, false)
	} else {
		ds, err = p.Visible(ctx, s)
	}
	if err != nil {
		return screening.DatasetCatalog{}, err
	}
	limit := 100
	if op.Limit > 0 {
		limit = op.Limit
	}
	type binding struct {
		Ref      sourcedata.DatasetRef
		Revision uuid.UUID
	}
	current := []binding{}
	for _, d := range ds {
		current = append(current, binding{d.Ref, d.grant})
	}
	sort.Slice(current, func(i, j int) bool {
		if current[i].Ref.Scope == current[j].Ref.Scope {
			return current[i].Ref.ID < current[j].Ref.ID
		}
		return current[i].Ref.Scope < current[j].Ref.Scope
	})
	type snapshot struct {
		Binding  []binding `json:"binding"`
		Datasets []Dataset `json:"datasets"`
	}
	page := snapshot{Binding: current, Datasets: ds}
	position := int64(0)
	if op.Cursor != "" {
		id, e := uuid.Parse(op.Cursor)
		if e != nil {
			return screening.DatasetCatalog{}, sourcedata.ErrCursor
		}
		var saved []byte
		if e = p.DB.QueryRow(ctx, `SELECT selection,position FROM screening.source_read_cursors WHERE id=$1 AND scope_key=$2 AND operation=$3 AND expires_at>now()`, id, s.Key(), sourcedata.Provider+":catalog").Scan(&saved, &position); e != nil {
			return screening.DatasetCatalog{}, sourcedata.ErrCursor
		}
		if json.Unmarshal(saved, &page) != nil {
			return screening.DatasetCatalog{}, sourcedata.ErrCursor
		}
		a, _ := json.Marshal(page.Binding)
		b, _ := json.Marshal(current)
		if string(a) != string(b) || position < 0 || position > int64(len(page.Datasets)) {
			return screening.DatasetCatalog{}, sourcedata.ErrCursor
		}
	}
	end := int(position) + limit
	if end > len(page.Datasets) {
		end = len(page.Datasets)
	}
	next := ""
	if end < len(page.Datasets) {
		next = uuid.NewString()
		saved, _ := json.Marshal(page)
		if _, e := p.DB.Exec(ctx, `INSERT INTO screening.source_read_cursors(id,scope_key,operation,selection,position,watermark) VALUES($1,$2,$3,$4,$5,0)`, next, s.Key(), sourcedata.Provider+":catalog", saved, end); e != nil {
			return screening.DatasetCatalog{}, e
		}
	}
	raw, _ := json.Marshal(map[string]any{"contract_version": 2, "provider": sourcedata.Provider, "datasets": page.Datasets[position:end], "profile": sourcedata.Profile, "next_cursor": next})
	return screening.DatasetCatalog{RawPayload: raw}, nil
}
func (p Provider) GetFreshness(ctx context.Context, s provideraccess.Scope, op sourcedata.Operation) (screening.DatasetFreshness, error) {
	if op.Provider != sourcedata.Provider {
		return screening.DatasetFreshness{}, sourcedata.ErrInput
	}
	ds, err := p.selected(ctx, s, op.Selection, false)
	if err != nil {
		return screening.DatasetFreshness{}, err
	}
	checks := false
	for _, d := range ds {
		checks = checks || d.Source != nil
	}
	raw, _ := json.Marshal(map[string]any{"contract_version": 2, "provider": sourcedata.Provider, "datasets": ds, "source_check_supported": checks, "limitations": []string{"Publication time differs from external check time; collection and rights must be explicitly enabled; no publisher SLA is inferred"}})
	return screening.DatasetFreshness{RawPayload: raw}, nil
}
func (p Provider) GetDatasetDelta(ctx context.Context, s provideraccess.Scope, op sourcedata.Operation) (screening.DatasetDelta, error) {
	if op.Provider != sourcedata.Provider || op.Limit < 0 || op.Limit > 500 {
		return screening.DatasetDelta{}, sourcedata.ErrInput
	}
	ds, err := p.selected(ctx, s, op.Selection, false)
	if err != nil {
		return screening.DatasetDelta{}, err
	}
	type grant struct {
		Ref      sourcedata.DatasetRef
		Revision uuid.UUID
	}
	bindings := []grant{}
	keys := []map[string]any{}
	for _, d := range ds {
		bindings = append(bindings, grant{d.Ref, d.grant})
		keys = append(keys, map[string]any{"owner": d.owner, "dataset": d.Ref.ID})
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].Ref.Scope == bindings[j].Ref.Scope {
			return bindings[i].Ref.ID < bindings[j].Ref.ID
		}
		return bindings[i].Ref.Scope < bindings[j].Ref.Scope
	})
	bound, _ := json.Marshal(bindings)
	selection, _ := json.Marshal(keys)
	position := int64(0)
	watermark := int64(0)
	if op.Cursor != "" {
		id, e := uuid.Parse(op.Cursor)
		if e != nil {
			return screening.DatasetDelta{}, sourcedata.ErrCursor
		}
		var previous []byte
		if e = p.DB.QueryRow(ctx, `SELECT selection,position,watermark FROM screening.source_read_cursors WHERE id=$1 AND scope_key=$2 AND operation=$3 AND expires_at>now()`, id, s.Key(), sourcedata.Provider+":delta").Scan(&previous, &position, &watermark); e != nil {
			return screening.DatasetDelta{}, sourcedata.ErrCursor
		}
		var old any
		var fresh any
		json.Unmarshal(previous, &old)
		json.Unmarshal(bound, &fresh)
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(fresh)
		if string(a) != string(b) {
			return screening.DatasetDelta{}, sourcedata.ErrCursor
		}
	}
	if watermark == 0 {
		if err = p.DB.QueryRow(ctx, `SELECT coalesce(max(id),0) FROM screening.source_publications`).Scan(&watermark); err != nil {
			return screening.DatasetDelta{}, err
		}
	}
	limit := 100
	if op.Limit > 0 {
		limit = op.Limit
	}
	rows, err := p.DB.Query(ctx, `SELECT p.id,p.dataset_id,p.generation,p.created_at,p.owner_scope FROM screening.source_publications p JOIN jsonb_to_recordset($1::jsonb) AS d(owner uuid,dataset text) ON (p.owner_scope,p.dataset_id)=(d.owner,d.dataset) WHERE p.id>$2 AND p.id<=$3 ORDER BY p.id LIMIT $4`, selection, position, watermark, limit+1)
	if err != nil {
		return screening.DatasetDelta{}, err
	}
	type publication struct {
		ID         int64                 `json:"id"`
		Dataset    string                `json:"dataset"`
		Generation uuid.UUID             `json:"generation"`
		At         time.Time             `json:"published_at"`
		Ref        sourcedata.DatasetRef `json:"dataset_ref"`
	}
	events := []publication{}
	for rows.Next() {
		var event publication
		var owner uuid.UUID
		if err = rows.Scan(&event.ID, &event.Dataset, &event.Generation, &event.At, &owner); err != nil {
			rows.Close()
			return screening.DatasetDelta{}, err
		}
		event.Ref = sourcedata.DatasetRef{Scope: "global", ID: event.Dataset}
		if owner != uuid.Nil {
			event.Ref.Scope = "tenant"
		}
		events = append(events, event)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return screening.DatasetDelta{}, err
	}
	more := len(events) > limit
	if more {
		events = events[:limit]
	}
	if len(events) > 0 {
		position = events[len(events)-1].ID
	}
	nextWatermark := watermark
	if !more {
		nextWatermark = 0
	}
	next := uuid.NewString()
	if _, err = p.DB.Exec(ctx, `INSERT INTO screening.source_read_cursors(id,scope_key,operation,selection,position,watermark) VALUES($1,$2,$3,$4,$5,$6)`, next, s.Key(), sourcedata.Provider+":delta", bound, position, nextWatermark); err != nil {
		return screening.DatasetDelta{}, err
	}
	raw, _ := json.Marshal(map[string]any{"contract_version": 2, "publications": events})
	return screening.DatasetDelta{RawPayload: raw, NextCursor: next, HasMore: more, Changed: len(events) > 0}, nil
}
