package postgres

import (
	"context"
	"encoding/json"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

type AnalyticsRepository struct{ db queryable }

// One statement gives membership, cases, events and all aggregates one MVCC snapshot.
// No case detail or report/evidence payloads are loaded into the API process.
const analyticsSQL = `WITH permitted AS MATERIALIZED (
 SELECT i.id,i.name,i.sla_days FROM case_manager.inboxes i
 WHERE i.tenant_id=$1 AND ($5::uuid IS NULL OR i.id=$5)
 AND ($4::text='' OR EXISTS(SELECT 1 FROM case_manager.inbox_users u WHERE u.tenant_id=i.tenant_id AND u.inbox_id=i.id AND u.user_id=$4))
), cohort AS MATERIALIZED (
 SELECT c.id,c.inbox_id,p.name inbox_name,c.status,c.outcome,c.created_at,c.assigned_to,c.snoozed_until,p.sla_days
 FROM case_manager.cases c JOIN permitted p ON p.id=c.inbox_id
 WHERE c.tenant_id=$1 AND c.created_at >= $2 AND c.created_at < $3
), history AS (
 SELECT e.case_id,
 max(e.created_at) FILTER(WHERE (e.event_type='status_updated' AND e.new_value='closed') OR e.event_type='bulk_close') last_close,
 max(e.created_at) FILTER(WHERE (e.event_type='status_updated' AND e.new_value IN ('investigating','pending')) OR e.event_type='bulk_reopen') last_open,
 count(*) FILTER(WHERE e.event_type='case_escalated' AND e.created_at >= $2 AND e.created_at < $3) escalations,
 count(*) FILTER(WHERE e.event_type='case_snoozed' AND e.created_at >= $2 AND e.created_at < $3) snooze_events
 FROM case_manager.case_events e JOIN cohort c ON c.id=e.case_id
 WHERE e.tenant_id=$1 AND e.created_at<=statement_timestamp()
 AND e.event_type IN ('status_updated','bulk_close','bulk_reopen','case_escalated','case_snoozed')
 GROUP BY e.case_id
), facts AS MATERIALIZED (
 SELECT c.*,coalesce(h.escalations,0) escalations,coalesce(h.snooze_events,0) snooze_events,
 CASE WHEN c.status='closed' AND h.last_close>=c.created_at AND (h.last_open IS NULL OR h.last_close>=h.last_open)
 THEN extract(epoch FROM h.last_close-c.created_at) END close_seconds
 FROM cohort c LEFT JOIN history h ON h.case_id=c.id
), metrics AS (
 SELECT inbox_id,max(inbox_name) inbox_name,grouping(inbox_id) is_total,
 count(*) total,
 count(*) FILTER(WHERE status='pending') pending,
 count(*) FILTER(WHERE status='investigating') investigating,
 count(*) FILTER(WHERE status='closed') closed,
 count(*) FILTER(WHERE outcome='false_positive') false_positive,
 count(*) FILTER(WHERE outcome='valuable_alert') valuable_alert,
 count(*) FILTER(WHERE outcome='confirmed_risk') confirmed_risk,
 count(*) FILTER(WHERE outcome='unset') unset,
 count(*) FILTER(WHERE status<>'closed' AND snoozed_until>statement_timestamp()) snoozed,
 count(*) FILTER(WHERE status<>'closed' AND sla_days IS NOT NULL) sla_configured,
 count(*) FILTER(WHERE status<>'closed' AND sla_days IS NOT NULL AND created_at+sla_days*interval '24 hours'<statement_timestamp()) overdue,
 coalesce(sum(escalations),0) escalations,coalesce(sum(snooze_events),0) snooze_events,
 count(close_seconds) measured_closures,avg(close_seconds) average_close_seconds
 FROM facts GROUP BY GROUPING SETS ((inbox_id),())
), daily AS (
 SELECT to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD') date,count(*) total,
 count(*) FILTER(WHERE status='pending') pending,count(*) FILTER(WHERE status='investigating') investigating,count(*) FILTER(WHERE status='closed') closed
 FROM cohort GROUP BY 1
), assignments AS (
 SELECT inbox_id,assigned_to assignee,count(*) open FROM cohort WHERE status<>'closed' GROUP BY inbox_id,assigned_to
)
SELECT ($5::uuid IS NULL OR EXISTS(SELECT 1 FROM permitted)),jsonb_build_object(
 'as_of',statement_timestamp(),'from',$2::timestamptz,'to',$3::timestamptz,
 'totals',(SELECT to_jsonb(m)-'inbox_id'-'inbox_name'-'is_total' FROM metrics m WHERE is_total=1),
 'inboxes',coalesce((SELECT jsonb_agg(to_jsonb(m)-'is_total' ORDER BY inbox_id) FROM (SELECT * FROM metrics WHERE is_total=0 ORDER BY inbox_id LIMIT 1001) m),'[]'::jsonb),
 'daily',coalesce((SELECT jsonb_agg(to_jsonb(d) ORDER BY date) FROM daily d),'[]'::jsonb),
 'assignments',coalesce((SELECT jsonb_agg(to_jsonb(a) ORDER BY inbox_id,assignee NULLS FIRST) FROM (SELECT * FROM assignments ORDER BY inbox_id,assignee NULLS FIRST LIMIT 1001) a),'[]'::jsonb))`

func (r AnalyticsRepository) Analytics(ctx context.Context, tenant uuid.UUID, f casepkg.AnalyticsFilter) (casepkg.Analytics, error) {
	var result casepkg.Analytics
	var raw []byte
	var permitted bool
	if err := r.db.QueryRow(ctx, analyticsSQL, tenant, f.From, f.To, f.AccessUserID, f.InboxID).Scan(&permitted, &raw); err != nil {
		return result, err
	}
	if !permitted {
		return result, casepkg.ErrForbidden
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	if len(result.Inboxes) > 1000 || len(result.Assignments) > 1000 {
		return casepkg.Analytics{}, casepkg.Invalid("analytics has more than 1000 inbox or assignment groups; narrow the date range or select an inbox")
	}
	return result, nil
}
