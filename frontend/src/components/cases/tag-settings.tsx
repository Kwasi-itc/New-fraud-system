"use client";

import { useCaseContext, useCaseWrite } from "@/lib/case-manager-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { CaseNav, Panel, Problem, labelClass } from "./shared";

export function TagSettings({ tenant, session }: { tenant: string; session: string }) {
  const { identity, tags } = useCaseContext(tenant, session);
  const write = useCaseWrite(tenant, session);
  return <div className="space-y-6">
    <CaseNav tenant={tenant} /><h1 className="text-3xl font-semibold">Case tags</h1>
    <Problem error={identity.error ?? tags.error ?? write.error} />
    {identity.isPending && <p>Checking permissions…</p>}
    {identity.isSuccess && !identity.data.admin && <p role="alert">Tag settings require a tenant administrator.</p>}
    {identity.data?.admin && <>
      <Panel title="Create tag"><form className="flex flex-wrap items-end gap-3" onSubmit={async event => {
        event.preventDefault(); const form = event.currentTarget; const data = new FormData(form);
        try { await write.mutateAsync({ path: "tags", payload: { name: data.get("name"), color: data.get("color"), target: "case" } }); form.reset(); } catch { /* Displayed above. */ }
      }}>
        <label className={labelClass}>Name<Input name="name" required maxLength={200} /></label>
        <label className={labelClass}>Color<Input name="color" type="color" defaultValue="#2563eb" /></label>
        <Button type="submit" disabled={write.isPending}>Create tag</Button>
      </form></Panel>
      <Panel title="Active tags">
        <p className="text-sm text-slate-600">Archiving prevents new attachments. Existing cases retain the tag and its history.</p>
        {tags.isPending && <p>Loading tags…</p>}
        {tags.data?.tags.length === 0 && <p>No active tags.</p>}
        {tags.data?.tags.map(tag => <form key={`${tag.id}:${tag.name}:${tag.color}`} className="flex flex-wrap items-end gap-3 border-b border-slate-200 pb-4" onSubmit={async event => {
          event.preventDefault(); const data = new FormData(event.currentTarget);
          try { await write.mutateAsync({ path: `tags/${tag.id}`, method: "PATCH", payload: { name: data.get("name"), color: data.get("color") } }); } catch { /* Displayed above. */ }
        }}>
          <label className={labelClass}>Name<Input aria-label={`Name for ${tag.name}`} name="name" required maxLength={200} defaultValue={tag.name} /></label>
          <label className={labelClass}>Color<Input name="color" maxLength={64} defaultValue={tag.color} /></label>
          <Button type="submit" disabled={write.isPending}>Save tag</Button>
          <Button type="button" variant="outline" disabled={write.isPending} onClick={() => void write.mutateAsync({ path: `tags/${tag.id}`, method: "DELETE" }).catch(() => {})}>Archive {tag.name}</Button>
        </form>)}
      </Panel>
    </>}
  </div>;
}
