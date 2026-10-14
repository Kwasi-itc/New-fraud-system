"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { dataModelApi, type DisplaySettings } from "@/lib/data-model-api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export function DisplaySettingsButton({ tableId, tenant, name }: { tableId: string; tenant: string; name: string }) {
  const [open, setOpen] = useState(false);
  return <><Button variant="outline" onClick={() => setOpen(true)} aria-label={`Display settings for ${name}`}>Display settings</Button>{open && <DisplaySettingsModal tableId={tableId} tenant={tenant} onClose={() => setOpen(false)} />}</>;
}

function DisplaySettingsModal({ tableId, tenant, onClose }: { tableId: string; tenant: string; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [busy, setBusy] = useState(false);
  const query = useQuery({ queryKey: ["data-model", "display-settings", tenant, tableId], queryFn: () => dataModelApi.getDisplaySettings(tableId), staleTime: 0, gcTime: 0, retry: false, refetchOnWindowFocus: false });
  useEffect(() => { const element = dialog.current; const previous = document.activeElement as HTMLElement | null; element?.showModal(); return () => { element?.close(); previous?.focus(); }; }, []);
  return <dialog ref={dialog} aria-labelledby="display-title" className="m-auto max-h-[90vh] w-[min(56rem,calc(100vw-2rem))] overflow-auto rounded-2xl border border-slate-200 bg-white p-6 shadow-xl backdrop:bg-slate-950/40" onCancel={event => { event.preventDefault(); if (!busy) onClose(); }}>
    <h2 id="display-title" className="text-xl font-semibold">Display settings</h2>
    {query.isPending && <p className="py-6">Loading display settings…</p>}
    {query.isError && <p role="alert" className="py-6">{query.error.message} <button className="underline" onClick={() => void query.refetch()}>Retry</button></p>}
    {query.data && <DisplayForm key={query.dataUpdatedAt} initial={query.data} tenant={tenant} onClose={onClose} reload={() => void query.refetch()} busy={busy} setBusy={setBusy} />}
    {!query.data && <Button variant="outline" onClick={onClose}>Close</Button>}
  </dialog>;
}

function DisplayForm({ initial, tenant, onClose, reload, busy, setBusy }: { initial: DisplaySettings; tenant: string; onClose: () => void; reload: () => void; busy: boolean; setBusy: (busy: boolean) => void }) {
  const client = useQueryClient();
  const [draft, setDraft] = useState(() => ({ ...initial, displayed_fields: initial.displayed_fields ?? [], field_order: [...new Set([...(initial.field_order ?? []), ...initial.fields.map(f => f.id)])], field_display: initial.field_display ?? {} }));
  const [error, setError] = useState("");
  const submitting = useRef(false);
  const fields = draft.field_order.map(id => draft.fields.find(f => f.id === id)).filter(f => !!f);
  function move(id: string, offset: number) { setDraft(old => { const order = [...old.field_order], index = order.indexOf(id), target = index + offset; if (target < 0 || target >= order.length) return old; [order[index], order[target]] = [order[target], order[index]]; return { ...old, field_order: order }; }); }
  async function save(event: React.FormEvent) {
    event.preventDefault(); if (submitting.current) return; submitting.current = true; setBusy(true); setError("");
    try { await dataModelApi.saveDisplaySettings(draft.table_id, draft); await client.invalidateQueries({ queryKey: ["data-model"] }); await client.invalidateQueries({ queryKey: ["record-displays", tenant] }); onClose(); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Display settings could not be saved."); }
    finally { submitting.current = false; setBusy(false); }
  }
  return <form onSubmit={save} className="mt-4 space-y-5">
    <p className="text-sm text-slate-600">Choose how {draft.name} records appear in Decisions. Columns show current source values. Filter Decisions by this object type to see the selected columns.</p>
    {error && <div role="alert" className="rounded-lg bg-red-50 p-3 text-red-800">{error} <button type="button" className="underline" onClick={reload}>Reload saved settings</button></div>}
    <fieldset disabled={busy} className="space-y-4">
      <label className="block text-sm font-medium">Record caption<select className="mt-2 block w-full rounded-lg border p-2" value={draft.caption_field} onChange={event => setDraft(old => ({ ...old, caption_field: event.target.value }))}><option value="">Use object ID</option>{draft.fields.filter(f => f.data_type === "string").map(f => <option key={f.id} value={f.name}>{f.name}</option>)}</select></label>
      <p className="text-sm text-slate-600">Select up to 12 columns. Use the arrows to change their order. Linked values fall back to the original identifier when a caption is missing.</p>
      <div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead><tr><th className="p-2">Show</th><th className="p-2">Field</th><th className="p-2">Column label</th><th className="p-2">Display value</th><th className="p-2">Order</th></tr></thead><tbody>{fields.map((field, index) => {
        const choice = draft.field_display[field.id] ?? {}, checked = draft.displayed_fields.includes(field.id);
        return <tr key={field.id} className="border-t"><td className="p-2"><input type="checkbox" aria-label={`Show ${field.name}`} checked={checked} disabled={!checked && draft.displayed_fields.length >= 12} onChange={() => setDraft(old => ({ ...old, displayed_fields: checked ? old.displayed_fields.filter(id => id !== field.id) : [...old.displayed_fields, field.id] }))} /></td><td className="p-2 font-medium">{field.name}</td><td className="p-2"><Input aria-label={`Label for ${field.name}`} maxLength={100} placeholder={field.name} value={choice.label ?? ""} onChange={event => setDraft(old => ({ ...old, field_display: { ...old.field_display, [field.id]: { ...choice, label: event.target.value } } }))} /></td><td className="p-2"><select aria-label={`Display value for ${field.name}`} className="rounded border p-2" value={choice.link_id ?? ""} onChange={event => setDraft(old => ({ ...old, field_display: { ...old.field_display, [field.id]: { ...choice, link_id: event.target.value || undefined } } }))}><option value="">{Object.keys(field.enum_labels ?? {}).length ? "Enum label" : "Field value"}</option>{draft.links.filter(link => link.field_id === field.id).map(link => <option key={link.id} value={link.id}>{link.target_table}.{link.caption_field}</option>)}</select></td><td className="whitespace-nowrap p-2"><button type="button" aria-label={`Move ${field.name} up`} disabled={index === 0} className="px-2 disabled:opacity-30" onClick={() => move(field.id, -1)}>↑</button><button type="button" aria-label={`Move ${field.name} down`} disabled={index === fields.length - 1} className="px-2 disabled:opacity-30" onClick={() => move(field.id, 1)}>↓</button></td></tr>;
      })}</tbody></table></div>
    </fieldset>
    <div className="flex justify-end gap-3"><Button type="button" variant="outline" disabled={busy} onClick={onClose}>Cancel</Button><Button type="submit" disabled={busy}>{busy ? "Saving…" : "Save display settings"}</Button></div>
  </form>;
}
