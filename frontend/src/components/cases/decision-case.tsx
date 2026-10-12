"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { CaseRecord } from "@/lib/case-manager-api";
import { useCaseContext, useCaseQueue, useCaseWrite } from "@/lib/case-manager-query";
import { CaseNav, Panel, Problem, fieldClass, labelClass, readable } from "./shared";
import { Evidence } from "./evidence";
import { RecordExplorer } from "./record-explorer";

export function DecisionCase({ tenant, session, decisionId }: { tenant: string; session: string; decisionId: string }) {
  const router = useRouter();
  const { identity, inboxes } = useCaseContext(tenant, session);
  const membership = useCaseQueue(tenant, session, `decision_id=${encodeURIComponent(decisionId)}&include_snoozed=true`, identity.isSuccess);
  const [mode, setMode] = useState<"create" | "attach" | null>(null);
  const [search, setSearch] = useState("");
  const [target, setTarget] = useState("");
  const [confirm, setConfirm] = useState(false);
  const candidates = useCaseQueue(tenant, session, `status=pending&status=investigating&include_snoozed=true&name=${encodeURIComponent(search)}`, identity.isSuccess && mode === "attach");
  const write = useCaseWrite(tenant, session);
  const linked = !membership.error && !identity.error ? membership.data?.pages.flatMap(page => page.cases) ?? [] : [];
  const activeInboxes = inboxes.error ? [] : inboxes.data?.inboxes.filter(inbox => inbox.status === "active") ?? [];
  const choices = candidates.error ? [] : candidates.data?.pages.flatMap(page => page.cases).filter(item => item.type === "decision" && activeInboxes.some(inbox => inbox.id === item.inbox_id)) ?? [];
  const selected = choices.find(item => item.id === target);
  const detail = (id: string) => `/cases/${encodeURIComponent(id)}?${new URLSearchParams({ tenant })}`;
  const canChoose = identity.isSuccess && membership.isSuccess && !membership.error && !linked.length;
  return <div className="space-y-6">
    <CaseNav tenant={tenant} />
    <header><h1 className="text-3xl font-semibold">Decision investigation</h1><p className="mt-2 break-all text-sm text-slate-500">Decision {decisionId}</p></header>
    <Problem error={identity.error} retry={() => void identity.refetch()} />
    <Problem error={membership.error} retry={() => void membership.refetch()} />
    <Problem error={write.error} />
    {(identity.isPending || (identity.isSuccess && membership.isPending)) && <p role="status">Finding the case…</p>}
    {!!linked.length && <Panel title="Current case">{linked.map(item => <div key={item.id} className="space-y-3"><p><Link className="text-blue-700 underline" href={detail(item.id)}>{item.name}</Link> · {readable(item.status)}</p><Evidence tenant={tenant} session={session} caseId={item.id} kind="decisions" resource={decisionId} /></div>)}<p className="text-sm text-slate-500">This decision belongs to one primary case, including after closure. Open that case to investigate or explicitly transfer it.</p></Panel>}
    {linked.map(item => <RecordExplorer key={item.id} tenant={tenant} session={session} caseId={item.id} decisionId={decisionId} />)}
    {canChoose && <>
      <p>No accessible case found. Create an investigation or attach this decision to an existing open case. A decision already linked elsewhere cannot be attached again.</p>
      <div className="flex gap-3"><Button variant="outline" disabled={write.isPending} onClick={() => { setMode("create"); setConfirm(false); }}>Create a case</Button><Button variant="outline" disabled={write.isPending} onClick={() => { setMode("attach"); setConfirm(false); }}>Add to existing case</Button></div>
      <Problem error={inboxes.error} retry={() => void inboxes.refetch()} />
      {mode === "create" && <Panel title="Create decision investigation"><form className="space-y-4" onSubmit={async event => {
        event.preventDefault(); const data = new FormData(event.currentTarget);
        try {
          const result = await write.mutateAsync({ path: "cases", payload: { name: String(data.get("name")).trim(), inbox_id: data.get("inbox"), type: "decision", decision_ids: [decisionId] } }) as { case: CaseRecord };
          router.push(detail(result.case.id));
        } catch { await membership.refetch(); }
      }}><fieldset disabled={write.isPending || !inboxes.isSuccess} className="space-y-4">
        <label className={labelClass}>Case name<Input name="name" required maxLength={300} defaultValue={`Decision ${decisionId}`} onInput={event => event.currentTarget.setCustomValidity(event.currentTarget.value.trim() ? "" : "Enter a case name.")} /></label>
        <label className={labelClass}>Inbox<select name="inbox" className={fieldClass} required><option value="">Choose an inbox</option>{activeInboxes.map(inbox => <option key={inbox.id} value={inbox.id}>{inbox.name}</option>)}</select></label>
        <p className="text-sm text-slate-500">Creation starts an investigation and assigns you when you are an eligible inbox member. Existing capacity limits apply.</p>
        <Button type="submit" disabled={write.isPending || !activeInboxes.length}>{write.isPending ? "Creating…" : "Create investigation"}</Button>
      </fieldset></form></Panel>}
      {mode === "attach" && <Panel title="Select investigation"><form className="space-y-4" onSubmit={async event => {
        event.preventDefault(); if (!selected) return;
        if (!confirm) { setConfirm(true); return; }
        try { await write.mutateAsync({ path: `cases/${selected.id}/decisions`, payload: { decision_id: decisionId } }); router.push(detail(selected.id)); }
        catch { await membership.refetch(); }
      }}><fieldset disabled={write.isPending} className="space-y-4">
        <label className={labelClass}>Find case<Input value={search} onChange={event => { setSearch(event.target.value); setTarget(""); setConfirm(false); }} /></label>
        <Problem error={candidates.error} retry={() => void candidates.refetch()} />
        {candidates.isPending && <p role="status">Loading cases…</p>}
        <label className={labelClass}>Destination case<select className={fieldClass} required value={target} onChange={event => { setTarget(event.target.value); setConfirm(false); }}><option value="">Choose a case</option>{choices.map(item => <option key={item.id} value={item.id}>{item.name} · {item.id}</option>)}</select></label>
        {candidates.isSuccess && !choices.length && <p>No accessible open decision cases on this page.</p>}
        {candidates.hasNextPage && <Button type="button" variant="outline" disabled={candidates.isFetchingNextPage} onClick={() => void candidates.fetchNextPage()}>More cases</Button>}
        {confirm && selected && <p role="status">Attach decision {decisionId} to {selected.name}? This starts a pending investigation, may assign you if unassigned, and clears its attention flag. Its outcome and snooze time are preserved.</p>}
        <Button type="submit" disabled={!selected || write.isPending}>{write.isPending ? "Attaching…" : confirm ? "Confirm attachment" : "Review attachment"}</Button>
      </fieldset></form></Panel>}
    </>}
  </div>;
}
