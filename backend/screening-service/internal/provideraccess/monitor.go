package provideraccess

// MonitorAuthoritySQL is a read-only owner contract. A queued job has authority
// only while its explicit subscription, origin, current actor membership and
// tenant remain valid. It cannot expand the origin's query/selection or assume
// authority from a tenant ID in River args. Dataset grants are checked separately.
const MonitorAuthoritySQL = `SELECT EXISTS(
 SELECT 1 FROM screening.source_rescreen_targets t
 JOIN screening.source_monitors m ON (m.tenant_id,m.id)=(t.tenant_id,t.monitor_id)
 JOIN screening.screenings o ON (o.tenant_id,o.id)=(m.tenant_id,m.origin_screening_id)
 JOIN screening.screenings r ON (r.tenant_id,r.id)=(t.tenant_id,t.screening_id)
 JOIN core.tenants tenant ON tenant.id=m.tenant_id
 JOIN identity.memberships member ON member.tenant_id=m.tenant_id AND member.user_id::text=m.created_by
 JOIN identity.users actor ON actor.id=member.user_id
 WHERE t.tenant_id=$1 AND t.id=$2 AND t.status='queued' AND m.enabled
 AND tenant.status='active' AND member.active AND actor.active
 AND member.roles && ARRAY['case_investigator','case_manager_admin']::text[]
 AND o.provider='internal-watchlists' AND r.provider=o.provider AND o.is_manual
 AND o.idempotency_key LIKE 'manual:'||m.created_by||':%'
 AND r.idempotency_key='source-rescreen:'||t.id::text||':'||r.id::text AND NOT r.is_manual
 AND (r.request_json-'idempotency_key'-'is_manual'-'decision_id'-'scenario_id'-'screening_config_id')
  =(o.request_json-'idempotency_key'-'is_manual'-'decision_id'-'scenario_id'-'screening_config_id'))`
