"use client";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import type { CaseFile, CaseReport, ReportContent } from "@/lib/case-manager-api";
import { useCaseLinks, useCaseReports, useCaseWrite } from "@/lib/case-manager-query";
import { Panel, Problem, fieldClass, labelClass, timestamp } from "./shared";

export function CaseReports({ tenant, session, id }: { tenant: string; session: string; id: string }) {
  const reports = useCaseReports(tenant, session, id);
  const files = useCaseLinks<CaseFile>(tenant, session, id, "files");
  const write = useCaseWrite(tenant, session);
  const [draftID, setDraftID] = useState<string>();
  const evidence = files.error ? [] : files.data?.pages.flatMap(p => p.items).filter(f => f.storage_key?.startsWith("case-db/")) ?? [];
  return <Panel title="Suspicious activity reports">
    <p className="text-sm text-slate-600">Internal investigation reports. Completion preserves a snapshot; it does not submit a regulatory filing.</p>
    <Problem error={reports.error} retry={() => void reports.refetch()} />
    <Problem error={files.error} retry={() => void files.refetch()} />
    {reports.isPending && <p role="status">Loading reports...</p>}
    {!reports.error && reports.data && <>
      {reports.data.pages.flatMap(p => p.reports).map(report => <details key={report.id} className="rounded border border-slate-200 p-3">
        <summary className="cursor-pointer text-sm font-semibold">{report.payload.content?.title || "Legacy report"} · {report.status} · v{report.version}</summary>
        <div className="mt-3 space-y-3">
          <p className="text-xs text-slate-500">Created by {report.created_by}{report.completed_at && ` · Completed by ${report.completed_by ?? "unknown"} at ${timestamp(report.completed_at)}`}</p>
          {report.payload.format === "internal_sar_v1" ? <ReportEditor key={report.version} report={report} files={evidence} tenant={tenant} session={session} id={id} /> : <p className="text-sm">Legacy format. Download the original payload to inspect this report.</p>}
          <a className="text-sm text-blue-700" href={`/api/cases/tenants/${tenant}/cases/${id}/reports/${report.id}/export`}>Download report JSON</a>
        </div>
      </details>)}
      {!reports.data.pages.some(p => p.reports.length) && <p className="text-sm text-slate-500">No reports yet.</p>}
      {reports.hasNextPage && <Button variant="outline" disabled={reports.isFetchingNextPage} onClick={() => void reports.fetchNextPage()}>More reports</Button>}
      {files.hasNextPage && <Button variant="outline" disabled={files.isFetchingNextPage} onClick={() => void files.fetchNextPage()}>Load more report attachment choices</Button>}
      <Problem error={write.error} />
      {!draftID ? <Button variant="outline" onClick={() => setDraftID(crypto.randomUUID())}>New report</Button> : <form className="space-y-3" onSubmit={async event => {
        event.preventDefault(); const data = new FormData(event.currentTarget);
        try { await write.mutateAsync({ path: `cases/${id}/reports`, payload: { id: draftID, content: { title: data.get("title"), subject: "", narrative: "", file_ids: [] } } }); setDraftID(undefined); } catch { /* Mutation error remains visible; keep the retry ID and input. */ }
      }}><label className={labelClass}>New report title<input name="title" required maxLength={200} className={fieldClass} /></label><Button disabled={write.isPending}>Create draft</Button></form>}
    </>}
  </Panel>;
}

function ReportEditor({ report, files, tenant, session, id }: { report: CaseReport; files: CaseFile[]; tenant: string; session: string; id: string }) {
  const write = useCaseWrite(tenant, session);
  const content = report.payload.content;
  const [selected, setSelected] = useState<string[]>(content.file_ids ?? []);
  const [dirty, setDirty] = useState(false);
  const completed = report.status === "completed";
  return <form className="space-y-3" onChange={() => setDirty(true)} onSubmit={async event => {
    event.preventDefault(); const data = new FormData(event.currentTarget);
    const from = String(data.get("from")); const to = String(data.get("to"));
    const next: ReportContent = { title: String(data.get("title")), subject: String(data.get("subject")), narrative: String(data.get("narrative")), file_ids: selected,
      activity_from: from ? new Date(from + "Z").toISOString() : undefined, activity_to: to ? new Date(to + "Z").toISOString() : undefined };
    try { await write.mutateAsync({ path: `cases/${id}/reports/${report.id}`, method: "PATCH", payload: { version: report.version, content: next } }); } catch { /* Preserve edited content and show the conflict/failure. */ }
  }}>
    <Problem error={write.error} />
    <fieldset disabled={completed || write.isPending} className="space-y-3">
      <label className={labelClass}>Report title<input name="title" defaultValue={content.title} required maxLength={200} className={fieldClass} /></label>
      <label className={labelClass}>Report subject<textarea name="subject" defaultValue={content.subject} maxLength={1000} className={fieldClass} /></label>
      <label className={labelClass}>Report narrative<textarea name="narrative" defaultValue={content.narrative} maxLength={50000} rows={5} className={fieldClass} /></label>
      <div className="grid gap-3 sm:grid-cols-2"><label className={labelClass}>Activity start (UTC)<input type="datetime-local" step="1" name="from" defaultValue={content.activity_from ? new Date(content.activity_from).toISOString().slice(0, 19) : undefined} className={fieldClass} /></label><label className={labelClass}>Activity end (UTC)<input type="datetime-local" step="1" name="to" defaultValue={content.activity_to ? new Date(content.activity_to).toISOString().slice(0, 19) : undefined} className={fieldClass} /></label></div>
      <fieldset className="space-y-2"><legend className="text-sm font-medium">Attached case evidence</legend>
        {Array.from(new Set([...files.map(f => f.id), ...selected])).map(fileID => <label key={fileID} className="flex gap-2 text-sm"><input type="checkbox" checked={selected.includes(fileID)} onChange={event => setSelected(previous => event.target.checked ? [...previous, fileID] : previous.filter(v => v !== fileID))} />{files.find(f => f.id === fileID)?.file_name ?? fileID}</label>)}
        {!files.length && !selected.length && <p className="text-sm text-slate-500">Upload case evidence to attach it here.</p>}
      </fieldset>
      {!completed && <Button type="submit" size="sm" variant="outline">Save report draft</Button>}
    </fieldset>
    {!completed && <><p className="text-xs text-slate-500">Save changes before completing. Completed reports cannot be edited.</p><Button type="button" size="sm" disabled={dirty || write.isPending} onClick={() => void write.mutateAsync({ path: `cases/${id}/reports/${report.id}/complete`, payload: { version: report.version } }).catch(() => {})}>Complete report</Button></>}
  </form>;
}
