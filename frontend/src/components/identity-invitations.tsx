"use client";
import { useEffect, useRef, useState } from "react";
import { identityRoles, identityRoleLabels } from "@/lib/identity-roles";
type Invitation = { id: string; email: string; roles: string[]; status: string; delivery: string; expires_at: string; version: number };
export function IdentityInvitations() {
  const [items, setItems] = useState<Invitation[]>([]), [email, setEmail] = useState(""), [roles, setRoles] = useState(["case_investigator"]);
  const [after, setAfter] = useState(""), [next, setNext] = useState(""), [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true), [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState("");
  const request = useRef<{ key: string; id: string } | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    fetch(`/api/auth/invitations?after=${after || "00000000-0000-0000-0000-000000000000"}`, { cache: "no-store", signal: controller.signal }).then(async response => {
      if (!response.ok) throw new Error("Invitations could not be loaded. Check your access or ask your operator to enable invitation delivery.");
      return response.json();
    }).then(data => { setItems(data.items); setNext(data.next_after); }).catch(e => { if (!controller.signal.aborted) setError(e.message); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [after, revision]);
  function refresh(cursor = after) { setLoading(true); setAfter(cursor); setRevision(v => v + 1); }
  async function mutate(action: string, item?: Invitation) {
    if (!window.confirm(action === "create" ? `Invite ${email} with the selected roles?` : `${action === "cancel" ? "Cancel" : "Resend"} invitation for ${item?.email}? Previous links will stop working.`)) return;
    setBusy(true); setError(""); setNotice("");
    const key = JSON.stringify([email, [...roles].sort()]);
    if (!request.current || request.current.key !== key) request.current = { key, id: crypto.randomUUID() };
    try {
      const response = await fetch("/api/auth/invitations", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(action === "create" ? { action, id: request.current.id, email, roles } : { action, id: item?.id, version: item?.version }) });
      if (!response.ok) throw new Error(response.status === 409 ? "The invitation changed, already exists, or was recently sent. Refresh the list before retrying." : "Invitation update failed. Check your access and try again.");
      setNotice(action === "cancel" ? "Invitation cancelled." : "Invitation queued for email delivery. Check delivery status below.");
      if (action === "create") { setEmail(""); request.current = null; }
      refresh("");
    } catch (e) { setError(e instanceof Error ? e.message : "Invitation update failed."); }
    finally { setBusy(false); }
  }
  return <section className="space-y-4"><h2 className="text-xl font-semibold">Invitations</h2>
    <form className="space-y-3" onSubmit={e => { e.preventDefault(); void mutate("create"); }}>
      <label className="block">Email address <input className="rounded border p-2" type="email" required maxLength={254} value={email} disabled={busy} onChange={e => setEmail(e.target.value)} /></label>
      <fieldset disabled={busy}><legend>Roles to assign</legend>{identityRoles.map(role => <label className="mr-4 inline-flex gap-2" key={role}><input type="checkbox" checked={roles.includes(role)} onChange={e => setRoles(v => e.target.checked ? [...v, role] : v.filter(r => r !== role))} />{identityRoleLabels[role]}</label>)}</fieldset>
      <button className="rounded bg-blue-800 px-4 py-2 text-white" disabled={busy || !roles.length}>Invite user</button>
    </form>
    {error && <p role="alert">{error}</p>}{notice && <p role="status">{notice}</p>}
    <button className="underline" disabled={busy || loading} onClick={() => { setError(""); refresh(); }}>Refresh invitations</button>
    {loading ? <p>Loading invitations…</p> : <ul className="space-y-3">{items.map(item => <li className="rounded border p-3" key={item.id}><p>{item.email} — {item.status}</p><p>{item.roles.map(r => identityRoleLabels[r] ?? r).join(", ")}</p><p>Expires: {new Date(item.expires_at).toLocaleString()}</p><p>Email: {item.delivery === "sent" ? "Accepted by mail server" : item.delivery}</p>{["pending", "expired"].includes(item.status) && <div className="flex gap-4"><button className="underline" disabled={busy} onClick={() => void mutate("resend", item)}>Resend</button><button className="underline" disabled={busy} onClick={() => void mutate("cancel", item)}>Cancel</button></div>}</li>)}</ul>}
    <div className="flex gap-4"><button disabled={busy || loading || !after} onClick={() => refresh("")}>First invitations</button><button disabled={busy || loading || !next} onClick={() => refresh(next)}>Next invitations</button></div>
  </section>;
}
