"use client";
import { useEffect, useState } from "react";
import { identityRoles as roles, identityRoleLabels as labels } from "@/lib/identity-roles";
type Member = { user_id: string; name: string; roles: string[]; active: boolean; version: number };
export function IdentityMembers() {
  const [items, setItems] = useState<Member[]>([]), [after, setAfter] = useState(""), [next, setNext] = useState("");
  const [error, setError] = useState(""), [loading, setLoading] = useState(true), [busy, setBusy] = useState("");
  const [revision, setRevision] = useState(0);
  useEffect(() => { const abort = new AbortController();
    fetch(`/api/auth/members?after=${encodeURIComponent(after || "00000000-0000-0000-0000-000000000000")}`, { signal: abort.signal, cache: "no-store" })
      .then(async response => { if (!response.ok) throw new Error("Accounts could not be loaded. Your session or permissions may have changed."); return response.json(); })
      .then(data => { if (!abort.signal.aborted) { setItems(data.items); setNext(data.next_after); } }).catch(e => { if (!abort.signal.aborted) setError(e.message); }).finally(() => { if (!abort.signal.aborted) setLoading(false); }); return () => abort.abort();
  }, [after, revision]);
  async function save(member: Member) {
    if (!window.confirm(`Save access for ${member.name}? Their current sessions will be revoked.`)) return;
    setBusy(member.user_id); setError("");
    try { const response = await fetch("/api/auth/members", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(member) });
      if (!response.ok) { const data = await response.json(); throw new Error(data.error ?? "Update failed."); } setLoading(true); setRevision(v => v + 1);
    } catch (e) { setError(e instanceof Error ? e.message : "Update failed."); } finally { setBusy(""); }
  }
  return <section className="space-y-4"><h2 className="text-xl font-semibold">Tenant accounts</h2><p>Account permissions are separate from Case Manager inbox membership.</p>
    {error && <p role="alert" className="text-red-700">{error}</p>}{loading ? <p>Loading accounts…</p> : items.length === 0 ? <p>No accounts found.</p> : items.map(member => <fieldset key={member.user_id} disabled={!!busy} className="rounded border p-4 space-y-2"><legend className="font-semibold">{member.name}</legend>
      {roles.map(role => <label key={role} className="mr-4 inline-flex gap-2"><input type="checkbox" checked={member.roles.includes(role)} onChange={e => setItems(values => values.map(v => v.user_id === member.user_id ? { ...v, roles: e.target.checked ? [...v.roles, role] : v.roles.filter(r => r !== role) } : v))} />{labels[role]}</label>)}
      <label className="flex gap-2"><input type="checkbox" checked={member.active} onChange={e => setItems(values => values.map(v => v.user_id === member.user_id ? { ...v, active: e.target.checked } : v))} />Account active in this tenant</label>
      <button className="rounded bg-blue-800 text-white px-4 py-2" disabled={!member.roles.length} onClick={() => save(member)}>{busy === member.user_id ? "Saving…" : "Save access"}</button>
    </fieldset>)}
    <div className="flex gap-4"><button disabled={!after || loading || !!busy} onClick={() => { setLoading(true); setError(""); setAfter(""); }}>First page</button><button disabled={!next || loading || !!busy} onClick={() => { setLoading(true); setError(""); setAfter(next); }}>Next page</button></div>
  </section>;
}
