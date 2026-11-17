package providerregistry

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
)

// Only the continuous worker calls this with rows read from owner repositories.
// It checks an enabled persisted config/object/tenant and exact field-map
// snapshot. The authenticated operator already owns configuration mutations.
func (r Registry) ForContinuousObject(ctx context.Context, config screening.ContinuousConfig, object screening.MonitoredObject) (context.Context, error) {
	tenant, e := uuid.Parse(object.TenantID)
	id, ie := uuid.Parse(object.ID)
	if e != nil || ie != nil || config.TenantID != object.TenantID || config.ID != object.ConfigID {
		return ctx, provideraccess.ErrScope
	}
	mapping, e := sourcedata.DecodeContinuous(config.FieldMapJSON)
	if e != nil {
		return ctx, e
	}
	var allowed bool
	e = r.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening.monitored_objects o JOIN screening.continuous_screening_configs c ON(c.tenant_id,c.id)=(o.tenant_id,o.config_id) JOIN core.tenants t ON t.id=o.tenant_id WHERE o.tenant_id=$1 AND o.id=$2 AND c.id=$3 AND c.enabled AND c.provider='internal-watchlists' AND c.field_map_json=$4::jsonb AND c.object_type=o.object_type AND t.status='active' AND o.status IN('pending','active'))`, tenant, id, config.ID, config.FieldMapJSON).Scan(&allowed)
	if e != nil || !allowed {
		return ctx, provideraccess.ErrScope
	}
	refs := []string{}
	for _, ref := range mapping.Selection.Datasets {
		refs = append(refs, ref.Scope+":"+ref.ID)
	}
	return provideraccess.WithScope(ctx, provideraccess.ContinuousObject(tenant, id, refs)), nil
}

func (r Registry) continuousRun(ctx context.Context, item screening.Screening, raw json.RawMessage) (context.Context, error) {
	var authority struct {
		Object   string          `json:"object_id"`
		Snapshot json.RawMessage `json:"config_snapshot"`
	}
	if json.Unmarshal(raw, &authority) != nil {
		return ctx, provideraccess.ErrScope
	}
	var config screening.ContinuousConfig
	var object screening.MonitoredObject
	var request json.RawMessage
	e := r.DB.QueryRow(ctx, `SELECT c.id::text,c.tenant_id::text,c.provider,c.field_map_json,o.id::text,o.config_id::text,o.tenant_id::text,o.object_type,o.object_id,s.request_json FROM screening.continuous_runs run JOIN screening.screenings s ON(s.tenant_id,s.id)=(run.tenant_id,run.screening_id) JOIN screening.monitored_objects o ON(o.tenant_id,o.id)=(run.tenant_id,run.monitored_object_id) JOIN screening.continuous_screening_configs c ON(c.tenant_id,c.id)=(o.tenant_id,o.config_id) WHERE run.tenant_id=$1 AND run.screening_id=$2 AND o.id::text=$3 AND c.field_map_json=$4::jsonb`, item.TenantID, item.ID, authority.Object, authority.Snapshot).Scan(&config.ID, &config.TenantID, &config.Provider, &config.FieldMapJSON, &object.ID, &object.ConfigID, &object.TenantID, &object.ObjectType, &object.ObjectID, &request)
	if e != nil {
		return ctx, provideraccess.ErrScope
	}
	var q screening.SearchRequest
	if json.Unmarshal(request, &q) != nil || q.Provider != config.Provider || q.ObjectType != object.ObjectType || q.ObjectID != object.ObjectID || q.DecisionID != "" || q.IsManual {
		return ctx, provideraccess.ErrScope
	}
	mapping, e := sourcedata.DecodeContinuous(config.FieldMapJSON)
	if e != nil {
		return ctx, e
	}
	a, _ := json.Marshal(mapping.Selection)
	var left, right any
	json.Unmarshal(a, &left)
	json.Unmarshal(q.ProviderConfig, &right)
	l, _ := json.Marshal(left)
	v, _ := json.Marshal(right)
	if string(l) != string(v) {
		return ctx, provideraccess.ErrScope
	}
	return r.ForContinuousObject(ctx, config, object)
}
