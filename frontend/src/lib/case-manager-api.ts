export type CaseStatus = "pending" | "investigating" | "closed";
export type CaseOutcome = "unset" | "false_positive" | "valuable_alert" | "confirmed_risk";
export type Tag = { id: string; name: string; color: string; deleted_at?: string };
export type Inbox = { id: string; name: string; status: string; escalation_inbox_id?: string; auto_assign_enabled: boolean; sla_days?: number };
export type InboxUser = { user_id: string; auto_assign_enabled: boolean; capacity: number };
export type CaseRecord = {
  id: string; tenant_id: string; inbox_id: string; name: string; status: CaseStatus; outcome: CaseOutcome;
  type: "decision" | "continuous_screening"; assigned_to?: string; snoozed_until?: string;
  sla_due_at?: string; boost_reason?: string; review_level?: string; created_at: string; updated_at: string;
  tags?: Tag[]; contributors?: { user_id: string; created_at: string }[];
};
export type CaseEvent = { id: string; event_type: string; user_id?: string; additional_note: string; new_value: string; previous_value: string; created_at: string };
export type DecisionLink = { id: string; decision_id: string; object_id: string; object_type: string; pivot_value?: string; created_at: string };
export type ScreeningLink = { id: string; screening_id: string; match_id?: string; status: string; created_at: string };
export type CaseFile = { storage_key?: string; id: string; file_name: string; content_type: string; file_size: number; uploaded_by: string; created_at: string };
export type QueuePage = { cases: CaseRecord[]; counts: Partial<Record<CaseStatus, number>>; next_cursor?: string };
export type LinkPage<T> = { items: T[]; next_cursor?: string };
export type ReportContent = { title: string; subject: string; narrative: string; activity_from?: string; activity_to?: string; file_ids: string[] };
export type CaseReport = { id: string; status: string; version: number; created_by: string; completed_by?: string; completed_at?: string; payload: { format: string; content: ReportContent }; legacy_payload?: unknown };
export type ReportPage = { reports: CaseReport[]; next_cursor?: string };
export type AnalyticsCounts = { total: number; pending: number; investigating: number; closed: number; false_positive: number; valuable_alert: number; confirmed_risk: number; unset: number; snoozed: number; sla_configured: number; overdue: number; escalations: number; snooze_events: number; measured_closures: number; average_close_seconds: number | null };
export type CaseAnalytics = { as_of: string; from: string; to: string; totals: AnalyticsCounts; inboxes: (AnalyticsCounts & { inbox_id: string; inbox_name: string })[]; daily: { date: string; total: number; pending: number; investigating: number; closed: number }[]; assignments: { inbox_id: string; assignee: string | null; open: number }[] };
export type CaseSession = { subject: string; tenant_id: string; admin: boolean };

export class CaseApiError extends Error {
  constructor(message: string, readonly status: number) { super(message); }
}
export async function requestCase<T>(tenant: string, path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`/api/cases/tenants/${encodeURIComponent(tenant)}/${path}`, {
    ...init, headers: { "Content-Type": "application/json" }, cache: "no-store", credentials: "same-origin",
  });
  const data = response.status === 204 ? undefined : await response.json();
  if (!response.ok) throw new CaseApiError(typeof data?.error === "string" ? data.error : data?.error?.message ?? "Case request failed.", response.status);
  return data as T;
}
export function writeCase<T>(tenant: string, path: string, payload: unknown = {}, method = "POST") {
  return requestCase<T>(tenant, path, { method, body: JSON.stringify(payload) });
}
