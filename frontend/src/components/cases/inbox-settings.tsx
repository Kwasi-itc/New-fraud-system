"use client";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useCaseContext, useCaseQuery, useCaseWrite } from "@/lib/case-manager-query";
import type { Inbox, InboxUser } from "@/lib/case-manager-api";
import { CaseNav, Panel, Problem, fieldClass, labelClass } from "./shared";

export function InboxSettings({ tenant, session }: { tenant: string; session: string }) {
  const { identity, inboxes } = useCaseContext(tenant, session);
  const write = useCaseWrite(tenant, session);
  const [selected, setSelected] = useState("");
  const users = useCaseQuery<{ users: InboxUser[] }>(tenant, session, `inboxes/${selected}/users`, !!selected && !!identity.data?.admin);
  const inbox = inboxes.data?.inboxes.find(item => item.id === selected);
  if (identity.error) return <Problem error={identity.error} retry={() => void identity.refetch()} />;
  return <div className="space-y-6"><CaseNav tenant={tenant} /><h1 className="text-3xl font-semibold">Inbox settings</h1>
    {identity.isPending && <p>Checking permissions…</p>}
    {identity.isSuccess && !identity.data.admin && <p role="alert">Inbox settings require a tenant administrator.</p>}
    {identity.data?.admin && <><Problem error={write.error ?? inboxes.error} />
      <Panel title="Create inbox"><form className="flex items-end gap-3" onSubmit={async event => { event.preventDefault(); const form = event.currentTarget; try { const result = await write.mutateAsync({ path: "inboxes", payload: { name: new FormData(form).get("name") } }) as { inbox: Inbox }; setSelected(result.inbox.id); form.reset(); } catch { /* Shown above. */ } }}><label className={labelClass}>Name<Input name="name" required /></label><Button type="submit" disabled={write.isPending}>Create inbox</Button></form></Panel>
      <label className={labelClass}>Inbox<select className={fieldClass} value={selected} onChange={event => setSelected(event.target.value)}><option value="">Select an inbox</option>{inboxes.data?.inboxes.map(item => <option key={item.id} value={item.id}>{item.name} ({item.status})</option>)}</select></label>
      {inbox && <><Panel title="Routing and availability"><form key={`${inbox.id}:${inbox.status}:${inbox.escalation_inbox_id}:${inbox.sla_days}:${inbox.auto_assign_enabled}`} className="grid gap-4 md:grid-cols-2" onSubmit={async event => { event.preventDefault(); const data = new FormData(event.currentTarget); try { await write.mutateAsync({ path: `inboxes/${selected}`, method: "PATCH", payload: { name: data.get("name"), status: data.get("status"), escalation_inbox_id: data.get("destination") || null, sla_days: data.get("sla_days") ? Number(data.get("sla_days")) : null, auto_assign_enabled: data.get("auto_assign") === "on" } }); } catch { /* Shown above. */ } }}>
        <label className={labelClass}>Name<Input name="name" defaultValue={inbox.name} required /></label><label className={labelClass}>Status<select name="status" defaultValue={inbox.status} className={fieldClass}><option value="active">Active</option><option value="archived">Archived</option></select></label>
        <label className={labelClass}>Escalation destination<select name="destination" defaultValue={inbox.escalation_inbox_id ?? ""} className={fieldClass}><option value="">No escalation destination</option>{inboxes.data?.inboxes.filter(item => item.id !== inbox.id && item.status === "active").map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
        <label className={labelClass}>SLA days (blank disables)<Input name="sla_days" type="number" min={1} max={3650} defaultValue={inbox.sla_days ?? ""} /></label><label className="text-sm"><input name="auto_assign" type="checkbox" defaultChecked={inbox.auto_assign_enabled} /> Automatically assign eligible members</label><p className="text-sm text-slate-500">Archived inboxes retain history. New cases, inbound moves, and reopening require an active inbox. Automatic assignment runs periodically. Automated reviews are not available yet.</p><Button type="submit" disabled={write.isPending}>Save inbox</Button>
      </form></Panel><Panel title="Inbox membership"><Problem error={users.error} retry={() => void users.refetch()} />{users.isPending && <p>Loading members…</p>}<ul className="space-y-2">{users.data?.users.map(user => <li key={user.user_id} className="flex items-center justify-between border-b py-2"><span>{user.user_id} ? capacity {user.capacity} ? auto assignment {user.auto_assign_enabled ? "on" : "off"}</span><Button variant="outline" size="sm" disabled={write.isPending} onClick={() => void write.mutateAsync({ path: `inboxes/${selected}/users/${encodeURIComponent(user.user_id)}`, method: "DELETE" }).catch(() => {})}>Remove membership</Button></li>)}</ul>
        <form className="flex items-end gap-3" onSubmit={async event => { event.preventDefault(); const form = event.currentTarget; const user = String(new FormData(form).get("user")).trim(); try { await write.mutateAsync({ path: `inboxes/${selected}/users/${encodeURIComponent(user)}`, method: "PUT", payload: { auto_assign_enabled: new FormData(form).get("auto_assign") === "on", capacity: Number(new FormData(form).get("capacity")) } }); form.reset(); } catch { /* Shown above. */ } }}><label className={labelClass}>Identity provider subject<Input name="user" required /></label><label className={labelClass}>Open case capacity<Input name="capacity" type="number" min={0} max={1000} defaultValue={20} required /></label><label className="text-sm"><input name="auto_assign" type="checkbox" /> Eligible for automatic assignment</label><Button type="submit" disabled={write.isPending}>Add / update member</Button></form>
      </Panel></>}
    </>}
  </div>;
}
