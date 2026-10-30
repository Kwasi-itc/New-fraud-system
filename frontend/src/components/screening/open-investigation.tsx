"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";

export function OpenInvestigation({ tenant, scope, screening, candidate }: { tenant: string; scope: string; screening: string; candidate: string }) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [inbox, setInbox] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const inboxes = useQuery<{ inboxes: { id: string; name: string; status: string }[] }>({
    queryKey: ["manual-investigation-inboxes", scope], enabled: open, retry: false, gcTime: 0,
    queryFn: async () => { const res = await fetch(`/api/cases/tenants/${tenant}/inboxes`, { cache: "no-store" }); if (!res.ok) throw new Error("Unable to load permitted inboxes."); return res.json(); },
  });
  async function investigate() {
    if (!inbox || sending) return;
    setSending(true); setError("");
    try {
      const res = await fetch(`/api/cases/tenants/${tenant}/manual-screening-investigations`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ screening_id: screening, candidate_id: candidate, inbox_id: inbox }) });
      const body = await res.json();
      if (!res.ok) throw new Error(res.status === 409 ? "This search has changed or already belongs to another inbox. Refresh its details before continuing." : body.error ?? "Unable to open investigation. Retry the same request.");
      router.push(`/cases/${body.case.id}`);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to open investigation."); }
    finally { setSending(false); }
  }
  if (!open) return <Button type="button" variant="outline" className="mt-4" onClick={() => setOpen(true)}>Open investigation</Button>;
  const available = inboxes.data?.inboxes.filter(item => item.status === "active") ?? [];
  return <section aria-label="Open candidate investigation" className="mt-4 space-y-3 rounded-lg bg-slate-50 p-4 text-sm">
    <p>Open or reuse one investigation for this stored search. All its candidates will be linked; no provider search is rerun and no match is confirmed.</p>
    {inboxes.isPending && <p role="status">Loading permitted inboxes…</p>}
    {inboxes.isError && <p role="alert">Unable to load inboxes. <button type="button" className="underline" onClick={() => void inboxes.refetch()}>Retry inboxes</button></p>}
    {inboxes.isSuccess && available.length === 0 && <p>No active inboxes are available to your account.</p>}
    {!!available.length && <label className="block">Investigation inbox<select className="mt-2 block w-full rounded-lg border border-slate-300 p-2" value={inbox} disabled={sending} onChange={e => setInbox(e.target.value)}><option value="">Choose an inbox</option>{available.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}
    {error && <p role="alert" className="text-red-700">{error}</p>}
    <div className="flex flex-wrap gap-2"><Button type="button" disabled={sending || !inbox} onClick={() => void investigate()}>{sending ? "Opening…" : "Continue to investigation"}</Button><Button type="button" variant="outline" disabled={sending} onClick={() => setOpen(false)}>Cancel</Button></div>
  </section>;
}
