package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

type analyticsRepo struct{ scope }

// Aggregate in one statement snapshot; evidence/report bodies never enter this
// path. Limit grouped output, and reject rather than silently truncate it.
func (r analyticsRepo) Analytics(ctx context.Context, t uuid.UUID, f c.AnalyticsFilter) (c.Analytics, error) {
	out := c.Analytics{From: f.From, To: f.To, Inboxes: []c.InboxAnalytics{}, Daily: []c.DailyAnalytics{}, Assignments: []c.AssignmentAnalytics{}}
	condition := "i.tenant_id=?"
	a := []any{id(t)}
	if f.InboxID != nil {
		condition += " AND i.id=?"
		a = append(a, id(*f.InboxID))
	}
	if f.AccessUserID != "" {
		u, e := uuid.Parse(f.AccessUserID)
		if e != nil {
			return out, c.ErrForbidden
		}
		condition += " AND EXISTS(SELECT 1 FROM " + r.table("inbox_users") + " m WHERE m.tenant_id=i.tenant_id AND m.inbox_id=i.id AND m.user_id=?)"
		a = append(a, id(u))
	}
	a = append(a, id(t), f.From, f.To, f.From, f.To, f.From, f.To, id(t))
	counts := `COUNT(*) total,SUM(status='pending') pending,SUM(status='investigating') investigating,SUM(status='closed') closed,
 SUM(outcome='false_positive') false_positive,SUM(outcome='valuable_alert') valuable_alert,SUM(outcome='confirmed_risk') confirmed_risk,SUM(outcome='unset') unset,
 SUM(status<>'closed' AND COALESCE(snoozed_until>as_of,FALSE)) snoozed,SUM(status<>'closed' AND sla_days IS NOT NULL) sla_configured,
 SUM(status<>'closed' AND COALESCE(DATE_ADD(created_at,INTERVAL sla_days DAY)<as_of,FALSE)) overdue,
 COALESCE(SUM(escalations),0) escalations,COALESCE(SUM(snooze_events),0) snooze_events,COUNT(close_seconds) measured_closures,AVG(close_seconds) average_close_seconds`
	names := []string{"total", "pending", "investigating", "closed", "false_positive", "valuable_alert", "confirmed_risk", "unset", "snoozed", "sla_configured", "overdue", "escalations", "snooze_events", "measured_closures", "average_close_seconds"}
	pairs := []string{}
	for _, v := range names {
		pairs = append(pairs, "'"+v+"',"+v)
	}
	q := fmt.Sprintf(`WITH clock AS(SELECT CURRENT_TIMESTAMP(6) as_of),permitted AS(
 SELECT i.id,i.name,i.sla_days FROM %s i WHERE %s),cohort AS(
 SELECT c.id,c.inbox_id,p.name inbox_name,c.status,c.outcome,c.created_at,c.assigned_to,c.snoozed_until,p.sla_days,clock.as_of
 FROM %s c JOIN permitted p ON p.id=c.inbox_id CROSS JOIN clock WHERE c.tenant_id=? AND c.created_at>=? AND c.created_at<?),history AS(
 SELECT e.case_id,MAX(IF((e.event_type='status_updated' AND JSON_UNQUOTE(JSON_EXTRACT(e.payload,'$.new_value'))='closed') OR e.event_type='bulk_close',e.created_at,NULL)) last_close,
 MAX(IF((e.event_type='status_updated' AND JSON_UNQUOTE(JSON_EXTRACT(e.payload,'$.new_value')) IN('pending','investigating')) OR e.event_type='bulk_reopen',e.created_at,NULL)) last_open,
 SUM(e.event_type='case_escalated' AND e.created_at>=? AND e.created_at<?) escalations,
 SUM(e.event_type='case_snoozed' AND e.created_at>=? AND e.created_at<?) snooze_events
 FROM %s e JOIN cohort c ON c.id=e.case_id WHERE e.tenant_id=? AND e.created_at<=c.as_of GROUP BY e.case_id),facts AS(
 SELECT c.*,COALESCE(h.escalations,0) escalations,COALESCE(h.snooze_events,0) snooze_events,
 IF(c.status='closed' AND h.last_close>=c.created_at AND(h.last_open IS NULL OR h.last_close>=h.last_open),TIMESTAMPDIFF(MICROSECOND,c.created_at,h.last_close)/1000000,NULL) close_seconds
 FROM cohort c LEFT JOIN history h ON h.case_id=c.id),metrics AS(
 SELECT inbox_id,MAX(inbox_name) inbox_name,%s FROM facts GROUP BY inbox_id WITH ROLLUP),daily AS(
 SELECT DATE_FORMAT(created_at,'%%Y-%%m-%%d') date,COUNT(*) total,SUM(status='pending') pending,SUM(status='investigating') investigating,SUM(status='closed') closed FROM cohort GROUP BY 1),assignments AS(
 SELECT inbox_id,assigned_to assignee,COUNT(*) open FROM cohort WHERE status<>'closed' GROUP BY inbox_id,assigned_to)
 SELECT 'clock',CAST(as_of AS CHAR) FROM clock
 UNION ALL SELECT 'permitted',CAST(COUNT(*) AS CHAR) FROM permitted
 UNION ALL SELECT 'totals',JSON_OBJECT(%s) FROM metrics WHERE inbox_id IS NULL
 UNION ALL SELECT 'inboxes',data FROM(SELECT JSON_OBJECT('inbox_id',BIN_TO_UUID(inbox_id),'inbox_name',inbox_name,%s) data FROM metrics WHERE inbox_id IS NOT NULL ORDER BY inbox_id LIMIT 1001) m
 UNION ALL SELECT 'daily',JSON_OBJECT('date',date,'total',total,'pending',pending,'investigating',investigating,'closed',closed) FROM daily
 UNION ALL SELECT 'assignments',data FROM(SELECT JSON_OBJECT('inbox_id',BIN_TO_UUID(inbox_id),'assignee',assignee,'open',open) data FROM assignments ORDER BY inbox_id,assignee LIMIT 1001) a`, r.table("inboxes"), condition, r.table("cases"), r.table("case_events"), counts, strings.Join(pairs, ","), strings.Join(pairs, ","))
	rows, e := r.db.QueryContext(ctx, q, a...)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var raw []byte
		if e = rows.Scan(&kind, &raw); e != nil {
			return out, e
		}
		switch kind {
		case "clock":
			out.AsOf, e = time.ParseInLocation("2006-01-02 15:04:05.999999", string(raw), time.UTC)
		case "permitted":
			if f.InboxID != nil && string(raw) == "0" {
				return out, c.ErrForbidden
			}
		case "totals":
			e = json.Unmarshal(raw, &out.Totals)
		case "inboxes":
			var v c.InboxAnalytics
			e = json.Unmarshal(raw, &v)
			out.Inboxes = append(out.Inboxes, v)
		case "daily":
			var v c.DailyAnalytics
			e = json.Unmarshal(raw, &v)
			out.Daily = append(out.Daily, v)
		case "assignments":
			var v c.AssignmentAnalytics
			e = json.Unmarshal(raw, &v)
			out.Assignments = append(out.Assignments, v)
		}
		if e != nil {
			return out, e
		}
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if len(out.Inboxes) > 1000 || len(out.Assignments) > 1000 {
		return c.Analytics{}, c.Invalid("analytics has more than 1000 groups; narrow the range")
	}
	return out, nil
}
