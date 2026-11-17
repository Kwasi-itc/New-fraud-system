package postgres

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/lookup"
)

// Read-only owner contract with screening's shared source schema. Authentication
// and current authoring permissions must be checked before either function.
func internalScreeningProviders(ctx context.Context, q queryable, tenant string) ([]screening.ProviderOption, error) {
	var exists, fuzzy bool
	if err := q.QueryRow(ctx, `SELECT to_regclass('screening.source_datasets') IS NOT NULL,to_regclass('screening.source_names_fuzzy_lookup') IS NOT NULL`).Scan(&exists, &fuzzy); err != nil {
		return nil, err
	}
	item := screening.ProviderOption{ID: "internal-watchlists", Name: "Internal watchlists", Profiles: []string{"exact-name-v1"}, Datasets: []screening.DatasetOption{}}
	if !exists {
		return []screening.ProviderOption{item}, nil
	}
	if fuzzy {
		item.Profiles = append(item.Profiles, "name-fuzzy-v1", "name-token-v2", "identifier-evidence-v1")
	}
	rows, err := q.Query(ctx, `SELECT d.owner_scope::text,d.id,d.label,d.category,CASE WHEN v.status='published' AND v.normalizer=$2 AND (c.connector IS NULL OR c.ready) THEN d.active_generation::text END FROM screening.source_datasets d JOIN screening.source_grants g ON (g.owner_scope,g.dataset_id)=(d.owner_scope,d.id) AND g.tenant_id=$1 LEFT JOIN screening.source_versions v ON (v.owner_scope,v.dataset_id,v.id)=(d.owner_scope,d.id,d.active_generation) LEFT JOIN screening.source_readiness_v1 c ON (c.owner_scope,c.dataset_id)=(d.owner_scope,d.id) WHERE d.enabled AND (c.connector IS NULL OR c.rights_state='approved') AND (d.owner_scope='00000000-0000-0000-0000-000000000000' OR d.owner_scope=$1) ORDER BY d.owner_scope,d.id LIMIT 501`, tenant, lookup.Normalizer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d screening.DatasetOption
		var owner string
		if err = rows.Scan(&owner, &d.Ref.ID, &d.Name, &d.Category, &d.Generation); err != nil {
			return nil, err
		}
		d.Ref.Scope = "tenant"
		if owner == "00000000-0000-0000-0000-000000000000" {
			d.Ref.Scope = "global"
		}
		item.Datasets = append(item.Datasets, d)
		if d.Generation != nil {
			item.Configured = true
		}
	}
	if len(item.Datasets) > 500 {
		return nil, screening.ErrAuthoringConflict
	}
	return []screening.ProviderOption{item}, rows.Err()
}
func validateInternalScreeningSelection(ctx context.Context, q queryable, tenant string, r screening.AuthoringRule) error {
	var exists, fuzzy bool
	if err := q.QueryRow(ctx, `SELECT to_regclass('screening.source_datasets') IS NOT NULL,to_regclass('screening.source_names_fuzzy_lookup') IS NOT NULL`).Scan(&exists, &fuzzy); err != nil {
		return err
	}
	if !exists || (r.Profile != "exact-name-v1" && !fuzzy) {
		return screening.ErrMatchingUnavailable
	}
	refs, _ := json.Marshal(r.DatasetSelection)
	var count int
	err := q.QueryRow(ctx, `SELECT count(*) FROM jsonb_to_recordset($2::jsonb) AS ref(scope text,id text) JOIN screening.source_datasets d ON d.id=ref.id AND d.owner_scope=CASE WHEN ref.scope='global' THEN '00000000-0000-0000-0000-000000000000'::uuid ELSE $1::uuid END JOIN screening.source_grants g ON (g.owner_scope,g.dataset_id)=(d.owner_scope,d.id) AND g.tenant_id=$1 JOIN screening.source_versions v ON (v.owner_scope,v.dataset_id,v.id)=(d.owner_scope,d.id,d.active_generation) LEFT JOIN screening.source_readiness_v1 c ON (c.owner_scope,c.dataset_id)=(d.owner_scope,d.id) WHERE d.enabled AND v.status='published' AND v.normalizer=$3 AND (c.connector IS NULL OR c.ready)`, tenant, refs, lookup.Normalizer).Scan(&count)
	if err != nil {
		return err
	}
	if count != len(r.DatasetSelection) {
		return screening.ErrMatchingUnavailable
	}
	return nil
}
