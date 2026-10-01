"use client";
import Link from "next/link";
import { FileUpload } from "./file-upload";
import { CaseReports } from "./reports";
import { useRouter } from "next/navigation";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useCaseContext, useCaseLinks, useCaseQuery, useCaseQueue, useCaseWrite } from "@/lib/case-manager-query";
import type { CaseRecord, CaseEvent, CaseFile, DecisionLink, ScreeningLink, QueuePage } from "@/lib/case-manager-api";
import { CaseNav, Panel, Problem, fieldClass, labelClass, readable, timestamp } from "./shared";
import { Evidence } from "./evidence";

function ActionForm({ children, label, busy, action }: { children: ReactNode; label: string; busy: boolean; action: (data: FormData) => Promise<unknown> }) {
  return <form className="space-y-3" onSubmit={async event => { event.preventDefault(); const form = event.currentTarget; try { await action(new FormData(form)); form.reset(); } catch { /* Shared mutation state displays the failure. */ } }}><fieldset disabled={busy} className="space-y-3">{children}<Button type="submit" size="sm" variant="outline">{label}</Button></fieldset></form>;
}
export function CaseDetail({ tenant, session, id, queueFilters }: { tenant: string; session: string; id: string; queueFilters: string }) {
  const router = useRouter();
  const { identity, inboxes, tags } = useCaseContext(tenant, session);
  const detail = useCaseQuery<{ case: CaseRecord }>(tenant, session, `cases/${id}/overview`, identity.isSuccess);
  const write = useCaseWrite(tenant, session);
  const item = detail.data?.case;
  const cursor = item ? btoa(JSON.stringify({ id: item.id, at: item.created_at, boosted: !!item.boost_reason })).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "") : "";
  const next = useCaseQuery<QueuePage>(tenant, session, `case-queue?${queueFilters}&limit=1&cursor=${encodeURIComponent(cursor)}`, !!item && !!queueFilters);
  const related = useCaseQueue(tenant, session, `related_to=${id}&include_snoozed=true`, !!item);
  const submit = (path: string, payload: unknown = {}, method = "POST") => write.mutateAsync({ path: `cases/${id}${path}`, payload, method });
  if (identity.error || detail.error) return <div className="space-y-5"><CaseNav tenant={tenant} /><Problem error={identity.error ?? detail.error} retry={() => { void identity.refetch(); void detail.refetch(); }} /></div>;
  if (!item) return <p role="status">Loading case…</p>;
  const inbox = inboxes.data?.inboxes.find(inbox => inbox.id === item.inbox_id);
  return <div className="space-y-6">
    <CaseNav tenant={tenant} />
    <header className="flex flex-wrap justify-between gap-4"><div><p className="text-xs text-slate-500">{item.id}</p><h1 className="text-3xl font-semibold">{item.name}</h1><p className="mt-2 text-sm text-slate-600">{inbox?.name ?? item.inbox_id} · {readable(item.type)} · Created {timestamp(item.created_at)}</p></div>
      {next.data?.cases[0] && <Link className="text-blue-700" href={`/cases/${next.data.cases[0].id}?tenant=${tenant}&queue=${encodeURIComponent(queueFilters)}`}>Next case →</Link>}
    </header>
    <Problem error={write.error} /><Problem error={inboxes.error ?? tags.error ?? next.error} />
    <div className="grid gap-6 xl:grid-cols-[1fr_340px]">
      <div className="space-y-6">
        <Panel title="Investigation"><dl className="grid gap-4 text-sm sm:grid-cols-3">
          <div><dt className="text-slate-500">Lifecycle</dt><dd className="font-semibold capitalize">{readable(item.status)}</dd></div><div><dt className="text-slate-500">Outcome</dt><dd className="capitalize">{readable(item.outcome)}</dd></div><div><dt className="text-slate-500">Assignee</dt><dd>{item.assigned_to ?? "Unassigned"}</dd></div>
          <div><dt className="text-slate-500">Snoozed until</dt><dd>{item.snoozed_until ? timestamp(item.snoozed_until) : "Not snoozed"}</dd></div><div><dt className="text-slate-500">Attention</dt><dd>{readable(item.boost_reason)}</dd></div><div><dt className="text-slate-500">Review level</dt><dd>{readable(item.review_level)}</dd></div>
        <div><dt className="text-slate-500">SLA deadline</dt><dd>{item.sla_due_at ? timestamp(item.sla_due_at) : "No SLA"}</dd></div></dl><p className="text-sm">Contributors: {item.contributors?.map(person => person.user_id).join(", ") || "None yet"}</p>
          <div className="flex flex-wrap gap-2">{item.tags?.map(tag => <button key={tag.id} disabled={write.isPending} className="rounded-full border bg-slate-50 px-3 py-1 text-sm" aria-label={`Remove tag ${tag.name}${tag.deleted_at ? " (archived)" : ""}`} onClick={() => void submit(`/tags/${tag.id}`, {}, "DELETE").catch(() => {})}>{tag.name}{tag.deleted_at ? " (archived)" : ""} ×</button>)}</div>
        </Panel>
        <Panel title="Findings"><ActionForm label="Add comment" busy={write.isPending} action={data => submit("/comments", { comment: data.get("comment") })}><label className={labelClass}>Investigation note<textarea name="comment" required rows={3} className={fieldClass} /></label></ActionForm></Panel>
        <CaseRelationships tenant={tenant} session={session} id={id} />
        <CaseReports tenant={tenant} session={session} id={id} />
        <Panel title="Related cases"><Problem error={related.error} retry={() => void related.refetch()} />{related.isPending && <p>Loading related cases…</p>}{related.isSuccess && !related.data.pages.some(page => page.cases.length) && <p className="text-sm text-slate-500">No related cases in your permitted inboxes.</p>}<ul className="space-y-2">{!related.error && related.data?.pages.flatMap(page => page.cases).map(other => <li key={other.id}><Link className="text-blue-700" href={`/cases/${other.id}?tenant=${tenant}`}>{other.name}</Link> <span className="text-sm text-slate-500">{readable(other.status)}</span></li>)}</ul>{related.hasNextPage && <Button variant="outline" onClick={() => void related.fetchNextPage()} disabled={related.isFetchingNextPage}>More related cases</Button>}</Panel>
        <CaseTimeline tenant={tenant} session={session} id={id} />
      </div>
      <aside className="space-y-5">
        <Panel title="Case actions"><ActionForm label="Rename" busy={write.isPending} action={data => submit("", { name: data.get("name") }, "PATCH")}><label className={labelClass}>Case name<Input name="name" defaultValue={item.name} required /></label></ActionForm>
          {item.status !== "closed" ? <>
            <ActionForm label="Assign" busy={write.isPending} action={data => submit("/assign", { assignee_id: data.get("assignee") })}><label className={labelClass}>Inbox member ID<Input name="assignee" required /></label></ActionForm>
            <div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" disabled={write.isPending} onClick={() => void submit("/assign", { assignee_id: identity.data?.subject }).catch(() => {})}>Assign to me</Button><Button size="sm" variant="outline" disabled={write.isPending || !item.assigned_to} onClick={() => void submit("/unassign").catch(() => {})}>Unassign</Button></div>
            <ActionForm label="Move case" busy={write.isPending} action={data => submit("", { inbox_id: data.get("inbox") }, "PATCH")}><label className={labelClass}>Destination inbox<select name="inbox" required className={fieldClass}><option value="">Select destination</option>{inboxes.data?.inboxes.filter(i => i.status === "active" && i.id !== item.inbox_id).map(i => <option key={i.id} value={i.id}>{i.name}</option>)}</select></label></ActionForm>
            <Button variant="outline" size="sm" disabled={write.isPending || !inbox?.escalation_inbox_id} onClick={async () => { try { await submit("/escalate"); router.push(`/cases?tenant=${tenant}`); } catch { /* Display mutation error. */ } }}>Escalate to configured inbox</Button>
            <ActionForm label="Snooze" busy={write.isPending} action={data => submit("/snooze", { until: new Date(String(data.get("until"))).toISOString() })}><label className={labelClass}>Snooze until<Input name="until" type="datetime-local" required /></label></ActionForm>
            {item.snoozed_until && <Button variant="outline" size="sm" disabled={write.isPending} onClick={() => void submit("/unsnooze").catch(() => {})}>Unsnooze</Button>}
          </> : <div className="flex flex-wrap gap-2"><Button disabled={write.isPending} onClick={() => void submit("", { status: "investigating", outcome: "unset" }, "PATCH").catch(() => {})}>Reopen investigation</Button>{item.assigned_to && <Button variant="outline" disabled={write.isPending} onClick={() => void submit("/unassign").catch(() => {})}>Unassign</Button>}</div>}
          <ActionForm label="Add tag" busy={write.isPending} action={data => submit("/tags", { tag_id: data.get("tag") })}><label className={labelClass}>Tag<select name="tag" required className={fieldClass}><option value="">Choose tag</option>{tags.data?.tags.filter(tag => !item.tags?.some(t => t.id === tag.id)).map(tag => <option key={tag.id} value={tag.id}>{tag.name}{tag.deleted_at ? " (archived)" : ""}</option>)}</select></label></ActionForm>
          <ActionForm label="Set review level" busy={write.isPending} action={data => submit("", { review_level: data.get("review") || null }, "PATCH")}><label className={labelClass}>Review<select name="review" defaultValue={item.review_level ?? ""} className={fieldClass}><option value="">Unset</option>{["probable_false_positive", "investigate", "escalate"].map(level => <option key={level} value={level}>{readable(level)}</option>)}</select></label></ActionForm>
        </Panel>
        {item.status !== "closed" && <Panel title="Resolve case"><ActionForm label="Close case" busy={write.isPending} action={data => submit("/close", { outcome: data.get("outcome"), comment: data.get("comment") })}>
          <label className={labelClass}>Outcome<select name="outcome" required className={fieldClass}><option value="">Select outcome</option>{["false_positive", "valuable_alert", "confirmed_risk"].map(value => <option key={value} value={value}>{readable(value)}</option>)}</select></label><label className={labelClass}>Closing findings<textarea name="comment" rows={3} required className={fieldClass} /></label>
        </ActionForm></Panel>}
      </aside>
    </div>
  </div>;
}

function CaseRelationships({ tenant, session, id }: { tenant: string; session: string; id: string }) {
  const decisions = useCaseLinks<DecisionLink>(tenant, session, id, "decisions");
  const screenings = useCaseLinks<ScreeningLink>(tenant, session, id, "screenings");
  const files = useCaseLinks<CaseFile>(tenant, session, id, "files");
  return <>
    <Panel title="Linked decisions"><Problem error={decisions.error} retry={() => void decisions.refetch()} />{decisions.isPending && <p>Loading decisions…</p>}{decisions.isSuccess && decisions.data.pages[0].items.length === 0 && <p>No linked decisions.</p>}{!decisions.error && decisions.data?.pages.flatMap(page => page.items).map(link => <article className="space-y-3 border-b pb-4" key={link.id}>
      <p className="text-sm font-medium">Decision {link.decision_id}</p><Link className="text-sm text-blue-700" href={`/cases/objects?tenant=${tenant}&caseId=${id}&objectType=${encodeURIComponent(link.object_type)}&objectId=${encodeURIComponent(link.object_id)}`}>{link.object_type}: {link.object_id}</Link>
      <Evidence tenant={tenant} session={session} caseId={id} kind="decisions" resource={link.decision_id} />
    </article>)}{decisions.hasNextPage && <Button variant="outline" disabled={decisions.isFetchingNextPage} onClick={() => void decisions.fetchNextPage()}>More decisions</Button>}</Panel>
    <Panel title="Screening matches"><Problem error={screenings.error} retry={() => void screenings.refetch()} />{screenings.isPending && <p>Loading screenings…</p>}{screenings.isSuccess && screenings.data.pages[0].items.length === 0 && <p>No linked screenings.</p>}{!screenings.error && screenings.data?.pages.flatMap(page => page.items).map(link => <article className="space-y-3 border-b pb-4" key={link.id}><p className="text-sm">Match {link.match_id ?? "Unknown"} · {readable(link.status)}</p><Evidence tenant={tenant} session={session} caseId={id} kind="screenings" resource={link.screening_id} /></article>)}{screenings.hasNextPage && <Button variant="outline" disabled={screenings.isFetchingNextPage} onClick={() => void screenings.fetchNextPage()}>More screenings</Button>}</Panel>
    <Panel title="Evidence files"><FileUpload tenant={tenant} session={session} caseId={id} /><Problem error={files.error} retry={() => void files.refetch()} />{files.isPending && <p>Loading files…</p>}{files.isSuccess && files.data.pages[0].items.length === 0 && <p>No attached files.</p>}<ul>{!files.error && files.data?.pages.flatMap(page => page.items).map(file => <li className="border-b py-3 text-sm" key={file.id}>{file.storage_key?.startsWith("case-db/") ? <a className="text-blue-700 underline" href={`/api/cases/tenants/${tenant}/cases/${id}/files/${file.id}/download`}>{file.file_name}</a> : <span>{file.file_name} (source storage)</span>} · {file.content_type} · {file.file_size.toLocaleString()} bytes <span className="text-slate-500">{timestamp(file.created_at)} · {file.uploaded_by}</span></li>)}</ul>{files.hasNextPage && <Button variant="outline" onClick={() => void files.fetchNextPage()}>More files</Button>}</Panel>
  </>;
}
function CaseTimeline({ tenant, session, id }: { tenant: string; session: string; id: string }) {
  const events = useCaseLinks<CaseEvent>(tenant, session, id, "events");
  return <Panel title="Activity · newest first"><Problem error={events.error} retry={() => void events.refetch()} />{events.isPending && <p>Loading activity…</p>}<ol className="space-y-4">{!events.error && events.data?.pages.flatMap(page => page.items).map(event => <li className="border-l-2 border-blue-200 pl-4" key={event.id}><div className="text-sm font-medium capitalize">{readable(event.event_type)}</div><p className="text-xs text-slate-500">{timestamp(event.created_at)} · {event.user_id ?? "System"}</p>{event.additional_note && <p className="mt-2 whitespace-pre-wrap text-sm">{event.additional_note}</p>}{event.new_value && <p className="text-sm">{event.previous_value && `${auditValue(event.previous_value)} → `}{auditValue(event.new_value)}</p>}</li>)}</ol>{events.hasNextPage && <Button variant="outline" disabled={events.isFetchingNextPage} onClick={() => void events.fetchNextPage()}>Earlier activity</Button>}</Panel>;
}

function auditValue(value: string) {
  if (value.startsWith("{")) {
    try {
      const state = JSON.parse(value) as Partial<CaseRecord>;
      if (state.status && state.inbox_id) return `Status: ${readable(state.status)}; outcome: ${readable(state.outcome)}; assignee: ${state.assigned_to ?? "Unassigned"}; inbox: ${state.inbox_id}`;
    } catch { /* Older events retain their original text. */ }
  }
  return readable(value);
}
