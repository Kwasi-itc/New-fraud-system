"use client";

import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { requestCase, type Inbox } from "@/lib/case-manager-api";
import { identityRequest, type IdentityCapability } from "@/lib/identity-api";
import { AccountCard, AccountFrame, AccountModal, AccountNotice, AccountPagination, styles } from "./account/account-ui";

type Client = { id: string; name: string; scopes: string[]; active: boolean; version: number };
type Key = { client_id: string; kid: string; public_key_pem: string; active: boolean };
type Approval = { scope: string; requested_by: string; approved_by: string | null };
type Permissions = { canManage: boolean; canApprove: boolean; actor: string; tenant: string; canManageInboxes: boolean; capabilities: string[] };

export function IdentityClientManager(props: Permissions) {
  const scope = `${props.tenant}:${props.actor}`, cache = useQueryClient();
  const [after, setAfter] = useState(""), [selected, setSelected] = useState(""), [creating, setCreating] = useState(false);
  const [search, setSearch] = useState(""), [prefix, setPrefix] = useState("");
  useEffect(() => { const timer = setTimeout(() => setPrefix(search.trim()), 250); return () => clearTimeout(timer); }, [search]);
  const list = useQuery({ queryKey: ["identity-clients", scope, after, prefix], queryFn: ({ signal }) => identityRequest<{ clients: Client[] }>(`clients?${new URLSearchParams({ after, query: prefix })}`, { signal }) });
  const catalog = useQuery({ queryKey: ["identity-client-scopes", scope], queryFn: ({ signal }) => identityRequest<{ capabilities: IdentityCapability[] }>("client-scopes", { signal }) });
  const detail = useQuery({ queryKey: ["identity-client", scope, selected], queryFn: ({ signal }) => identityRequest<Client>(`clients/${selected}`, { signal }), enabled: !!selected });
  const clients = list.data?.clients.slice(0, 100) ?? [], next = (list.data?.clients.length ?? 0) > 100 ? clients[99].id : "";
  async function updated(client: Client) { cache.setQueryData(["identity-client", scope, client.id], client); await cache.invalidateQueries({ queryKey: ["identity-clients", scope] }); }
  return <AccountFrame title="API clients" description="Manage integration identities, public keys, and client scopes." capabilities={props.capabilities} action={props.canManage && <button className={`${styles.button} ${styles.primary}`} onClick={() => setCreating(true)}>Register client</button>}>
    {selected ? <><button className={styles.link} style={{ marginBottom: 18 }} onClick={() => setSelected("")}>All API clients</button>{detail.isPending || catalog.isPending ? <p className={styles.empty}>Loading client…</p> : detail.isError || catalog.isError ? <AccountNotice>{detail.error?.message ?? catalog.error?.message}</AccountNotice> : <ClientDetail key={selected} client={detail.data!} catalog={catalog.data!.capabilities} permissions={props} onUpdated={updated} />}</> : <>
      <AccountCard><div className={styles.toolbar}><input className={`${styles.input} ${styles.search}`} type="search" aria-label="Search API clients" placeholder="Search by name prefix…" maxLength={100} value={search} onChange={event => { setSearch(event.target.value); setAfter(""); }} /><span className={styles.hint}>Client registrations in this tenant</span></div>
        {list.isPending ? <p className={styles.empty}>Loading clients…</p> : list.isError ? <div className={styles.cardBody}><AccountNotice>{list.error.message}</AccountNotice><button className={styles.link} onClick={() => void list.refetch()}>Retry clients</button></div> : <div className={styles.tableWrap}><table className={styles.table}><thead><tr>{["Client", "Status", "Scopes", "Action"].map(label => <th scope="col" key={label}>{label}</th>)}</tr></thead><tbody>{!clients.length ? <tr><td className={styles.empty} colSpan={4}>No clients match these filters.</td></tr> : clients.map(client => <tr key={client.id}><td><p className={styles.cellTitle}>{client.name}</p><p className={`${styles.cellSub} ${styles.mono}`}>{client.id}</p></td><td><span className={`${styles.badge} ${client.active ? styles.green : styles.gray}`}>{client.active ? "Active" : "Disabled"}</span></td><td><div className={styles.capTags}>{client.scopes.map(value => <span className={styles.badge} key={value}>{value}</span>)}</div></td><td><button className={styles.link} aria-label={`Manage ${client.name}`} onClick={() => setSelected(client.id)}>Manage</button></td></tr>)}</tbody></table></div>}
        <AccountPagination after={after} next={next} busy={list.isFetching} onChange={setAfter} label="Clients page" />
      </AccountCard><div style={{ marginTop: 22 }}><AccountNotice kind="info">One client access token can carry approved scopes across the fraud system. Each API enforces the required scope, tenant, and resource permissions.</AccountNotice></div>
    </>}
    {creating && <ClientNameModal onClose={() => setCreating(false)} onSaved={async client => { await updated(client); setCreating(false); setSelected(client.id); }} />}
  </AccountFrame>;
}

function ClientNameModal({ client, onClose, onSaved }: { client?: Client; onClose: () => void; onSaved: (client: Client) => Promise<void> }) {
  const [name, setName] = useState(client?.name ?? ""), [busy, setBusy] = useState(false), [error, setError] = useState("");
  async function save() { setBusy(true); setError(""); try { const out = await identityRequest<Client>(client ? `clients/${client.id}` : "clients", { method: client ? "PUT" : "POST", body: JSON.stringify({ name: name.trim(), scopes: client?.scopes ?? [], active: client?.active ?? true, version: client?.version ?? 0 }) }); await onSaved(out); } catch (reason) { setError(reason instanceof Error ? reason.message : "Client could not be saved."); } finally { setBusy(false); } }
  return <AccountModal title={client ? "Edit client name" : "Register an API client"} onClose={onClose} busy={busy}><form onSubmit={event => { event.preventDefault(); void save(); }}>{error && <AccountNotice>{error}</AccountNotice>}<div className={styles.field}><label htmlFor="client-name">Client name</label><input id="client-name" className={styles.input} value={name} required maxLength={100} disabled={busy} onChange={event => setName(event.target.value)} /><p className={styles.hint}>Configure scopes and register a public key before exchanging client proofs.</p></div><div className={styles.formActions}><button className={styles.button} type="button" disabled={busy} onClick={onClose}>Cancel</button><button className={`${styles.button} ${styles.primary}`} disabled={busy || !name.trim()}>{busy ? "Saving…" : client ? "Save name" : "Register client"}</button></div></form></AccountModal>;
}

function ClientDetail({ client, catalog, permissions, onUpdated }: { client: Client; catalog: IdentityCapability[]; permissions: Permissions; onUpdated: (client: Client) => Promise<void> }) {
  const { canManage, canApprove, actor, tenant, canManageInboxes } = permissions, scope = `${tenant}:${actor}`, cache = useQueryClient();
  const [normalScopes, setNormalScopes] = useState(client.scopes.filter(value => catalog.some(cap => cap.name === value && cap.client === "normal")));
  const [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState("");
  const [confirming, setConfirming] = useState(false), [renaming, setRenaming] = useState(false);
  const approvals = useQuery({ queryKey: ["identity-client-approvals", scope, client.id], queryFn: ({ signal }) => identityRequest<{ approvals: Approval[] }>(`clients/${client.id}/scope-approvals`, { signal }) });
  async function mutate(action: () => Promise<void>, message: string) { setBusy(true); setError(""); setNotice(""); try { await action(); setNotice(message); await cache.invalidateQueries({ queryKey: ["identity-client-approvals", scope, client.id] }); } catch (reason) { setError(reason instanceof Error ? reason.message : "Client update failed. Refresh before retrying a conflict."); } finally { setBusy(false); } }
  async function change(patch: Partial<Pick<Client, "scopes" | "active">>) { const out = await identityRequest<Client>(`clients/${client.id}`, { method: "PUT", body: JSON.stringify({ name: client.name, scopes: client.scopes, active: client.active, version: client.version, ...patch }) }); await onUpdated(out); }
  return <div className={styles.stack}>
    {!confirming && error && <AccountNotice>{error}</AccountNotice>}{notice && <AccountNotice kind="success">{notice}</AccountNotice>}
    <AccountCard title={client.name} description="Client access within this tenant" action={<div className={`${styles.row} ${styles.wrap}`}><span className={`${styles.badge} ${client.active ? styles.green : styles.gray}`}>{client.active ? "Active" : "Disabled"}</span>{canManage && <><button className={styles.button} disabled={busy} onClick={() => setRenaming(true)}>Edit name</button><button className={`${styles.button} ${client.active ? styles.danger : ""}`} disabled={busy} onClick={() => { setError(""); setConfirming(true); }}>{client.active ? "Disable client" : "Enable client"}</button></>}</div>}>
      <div className={styles.profileFacts}>{[["Client ID", client.id], ["Access-token audience", "urn:fraud:system"], ["Token lifetime", "15 minutes"]].map(([label, value]) => <div key={label}><p className={styles.factLabel}>{label}</p><p className={`${styles.factValue} ${label === "Client ID" ? styles.mono : ""}`}>{value}</p></div>)}</div>
    </AccountCard>
    <div className={styles.clientGrid}><div className={styles.stack}><AccountCard title="Client scopes" description="Choose the operations this client can request."><form className={styles.cardBody} onSubmit={event => { event.preventDefault(); void mutate(() => change({ scopes: [...normalScopes, ...client.scopes.filter(value => catalog.some(cap => cap.name === value && cap.client === "sensitive"))] }), "Scopes saved. Changes apply to future token exchanges."); }}>
      {catalog.filter(cap => cap.client === "normal").map(cap => <label className={styles.scope} key={cap.name}><input type="checkbox" checked={normalScopes.includes(cap.name)} disabled={busy || !canManage} onChange={event => setNormalScopes(values => event.target.checked ? [...values, cap.name] : values.filter(value => value !== cap.name))} /><span><span className={styles.cellTitle}>{cap.name}</span><span className={styles.cellSub} style={{ display: "block" }}>{cap.name === "ingest:write" ? "Submit tenant records" : cap.name === "decisions:evaluate" ? "Request decision evaluations" : "Read tenant decisions"}</span></span></label>)}
      {canManage && <div className={styles.formActions}><button className={`${styles.button} ${styles.primary}`} disabled={busy}>Save scopes</button></div>}
    </form></AccountCard>
      <AccountCard title="Sensitive scopes"><div className={styles.cardBody}>
        {approvals.isError && <AccountNotice>{approvals.error.message}</AccountNotice>}
        {catalog.filter(cap => cap.client === "sensitive").map(cap => { const approval = approvals.data?.approvals.find(value => value.scope === cap.name), requested = client.scopes.includes(cap.name); return <div className={styles.scope} key={cap.name}><div style={{ flex: 1 }}><code className={styles.mono}>{cap.name}</code><p className={styles.cellSub}>{requested ? approval?.approved_by ? "Approved" : "Pending approval" : "Approval by a different human is required."}</p>{cap.name === "cases:read" && <p className={styles.cellSub}>Also requires explicit inbox grants.</p>}</div><div className={styles.stack} style={{ gap: 8 }}>
          {!requested && canManage && <button className={styles.link} disabled={busy || !client.active} onClick={() => void mutate(() => change({ scopes: [...client.scopes, cap.name] }), "Sensitive scope requested. Another authorized human must approve it.")}>Request approval</button>}
          {requested && approval && !approval.approved_by && canApprove && <button className={styles.link} disabled={busy || approval.requested_by === actor} onClick={() => void mutate(async () => { await identityRequest(`clients/${client.id}/scope-approvals/${encodeURIComponent(cap.name)}`, { method: "POST" }); }, "Scope approved.")}>{approval.requested_by === actor ? "Another operator must approve" : "Approve scope"}</button>}
          {requested && canManage && <button className={styles.link} disabled={busy} onClick={() => { if (window.confirm(`Remove ${cap.name}? Existing tokens expire normally.`)) void mutate(() => change({ scopes: client.scopes.filter(value => value !== cap.name) }), "Scope removed for future token exchanges."); }}>Remove scope</button>}
        </div></div>; })}
      </div></AccountCard>
    </div><div className={styles.stack}>
      {canManage && <ClientKeys client={client} scope={scope} />}
      {canManageInboxes && <ClientInboxes client={client} tenant={tenant} scope={scope} />}
    </div></div>
    {renaming && <ClientNameModal client={client} onClose={() => setRenaming(false)} onSaved={async out => { await onUpdated(out); setRenaming(false); }} />}
    {confirming && <AccountModal title={client.active ? "Disable this client?" : "Enable this client?"} busy={busy} onClose={() => setConfirming(false)}>{error && <AccountNotice>{error}</AccountNotice>}<p className={styles.muted}>{client.active ? "Future token exchanges will be rejected. Existing access tokens expire normally." : "This client can exchange proofs when it has an active key and approved scopes."}</p><div className={styles.formActions}><button className={styles.button} disabled={busy} onClick={() => setConfirming(false)}>Cancel</button><button className={`${styles.button} ${styles.danger}`} disabled={busy} onClick={() => void mutate(async () => { await change({ active: !client.active }); setConfirming(false); }, "Client status updated.")}>{client.active ? "Disable client" : "Enable client"}</button></div></AccountModal>}
  </div>;
}

function ClientKeys({ client, scope }: { client: Client; scope: string }) {
  const [after, setAfter] = useState(""), [adding, setAdding] = useState(false), [revoking, setRevoking] = useState<Key | null>(null);
  const [kid, setKid] = useState(""), [pem, setPem] = useState(""), [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState("");
  const cache = useQueryClient(), query = useQuery({ queryKey: ["identity-client-keys", scope, client.id, after], queryFn: ({ signal }) => identityRequest<{ keys: Key[] }>(`client-keys?client_id=${client.id}&after=${encodeURIComponent(after)}`, { signal }) });
  const keys = query.data?.keys.slice(0, 100) ?? [], next = (query.data?.keys.length ?? 0) > 100 ? keys[99].kid : "";
  async function mutate(revoke?: Key) { setBusy(true); setError(""); try { if (revoke) await identityRequest(`client-keys/${encodeURIComponent(revoke.kid)}?client_id=${client.id}`, { method: "DELETE" }); else await identityRequest("client-keys", { method: "POST", body: JSON.stringify({ client_id: client.id, kid, public_key_pem: pem }) }); setAdding(false); setRevoking(null); setKid(""); setPem(""); setNotice(revoke ? "Key revoked for future proofs. Existing access tokens expire normally." : "Public key registered."); await cache.invalidateQueries({ queryKey: ["identity-client-keys", scope, client.id] }); } catch (reason) { setError(reason instanceof Error ? reason.message : "Key update failed."); } finally { setBusy(false); } }
  return <AccountCard title="Public keys" description="At most two keys can be active. Register a replacement before revoking the old key." action={<button className={styles.button} disabled={busy || !client.active || keys.filter(key => key.active).length >= 2} onClick={() => { setError(""); setAdding(true); }}>Add key</button>}>
    {!adding && !revoking && error && <div className={styles.cardBody}><AccountNotice>{error}</AccountNotice></div>}{notice && <div className={styles.cardBody}><AccountNotice kind="success">{notice}</AccountNotice></div>}
    {query.isPending ? <p className={styles.empty}>Loading keys…</p> : query.isError ? <div className={styles.cardBody}><AccountNotice>{query.error.message}</AccountNotice></div> : !keys.length ? <p className={styles.empty}>No keys on this page.</p> : keys.map(key => <div className={styles.keyRow} key={key.kid}><div><code className={styles.mono}>{key.kid}</code><p className={styles.cellSub}>{key.active ? "RSA · Active" : "Revoked"}</p></div>{key.active && <button className={styles.link} disabled={busy} onClick={() => { setError(""); setRevoking(key); }}>Revoke</button>}</div>)}
    {(after || next) && <AccountPagination after={after} next={next} busy={busy || query.isFetching} onChange={setAfter} label="Keys page" />}
    {adding && <AccountModal title="Add a public key" busy={busy} onClose={() => setAdding(false)}><form onSubmit={event => { event.preventDefault(); void mutate(); }}>{error && <AccountNotice>{error}</AccountNotice>}<div className={styles.stack}><div className={styles.field}><label htmlFor="key-id">Key ID</label><input id="key-id" className={styles.input} required maxLength={100} value={kid} disabled={busy} onChange={event => setKid(event.target.value)} /></div><div className={styles.field}><label htmlFor="key-pem">RSA public key PEM</label><textarea id="key-pem" className={`${styles.textarea} ${styles.mono}`} required maxLength={8192} value={pem} disabled={busy} onChange={event => setPem(event.target.value)} /><p className={styles.hint}>Register an RSA public key of at least 2048 bits. The customer retains the private key.</p></div></div><div className={styles.formActions}><button className={styles.button} type="button" disabled={busy} onClick={() => setAdding(false)}>Cancel</button><button className={`${styles.button} ${styles.primary}`} disabled={busy}>Register public key</button></div></form></AccountModal>}
    {revoking && <AccountModal title="Revoke this key?" busy={busy} onClose={() => setRevoking(null)}>{error && <AccountNotice>{error}</AccountNotice>}<p className={styles.muted}>Future proofs signed with {revoking.kid} will be rejected. Existing access tokens expire normally.</p><div className={styles.formActions}><button className={styles.button} disabled={busy} onClick={() => setRevoking(null)}>Cancel</button><button className={`${styles.button} ${styles.danger}`} disabled={busy} onClick={() => void mutate(revoking)}>Revoke key</button></div></AccountModal>}
  </AccountCard>;
}

function ClientInboxes({ client, tenant, scope }: { client: Client; tenant: string; scope: string }) {
  const cache = useQueryClient();
  const queryKey = ["identity-client-inboxes", scope, client.id];
  const [pending, setPending] = useState<{ id: string; grant: boolean } | null>(null);
  const [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState("");
  const query = useQuery({ queryKey, queryFn: async ({ signal }) => { const [available, grants] = await Promise.all([requestCase<{ inboxes: Inbox[] }>(tenant, "inboxes", { signal }), requestCase<{ inbox_ids: string[] }>(tenant, `clients/${client.id}/inboxes`, { signal })]); return { inboxes: available.inboxes, granted: grants.inbox_ids }; } });
  async function change(id: string, grant: boolean) {
    setBusy(true); setPending({ id, grant }); setError(""); setNotice("");
    try {
      await requestCase(tenant, `clients/${client.id}/inboxes/${id}`, { method: grant ? "PUT" : "DELETE", body: "{}" });
      cache.setQueryData(queryKey, (current: typeof query.data) => current && ({ ...current, granted: grant ? [...new Set([...current.granted, id])] : current.granted.filter(value => value !== id) }));
      setNotice(grant ? "Inbox access granted." : "Inbox access revoked.");
      await query.refetch();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Inbox access update failed."); }
    finally { setPending(null); setBusy(false); }
  }
  return <AccountCard title="Case inbox access" description="Case reads require an approved cases:read scope and an explicit inbox grant."><div className={styles.cardBody}>
    {error && <AccountNotice>{error}</AccountNotice>}{notice && <AccountNotice kind="success">{notice}</AccountNotice>}
    {query.isPending ? <p className={styles.muted}>Loading inbox grants…</p> : query.isError ? <AccountNotice>{query.error.message}</AccountNotice> : !query.data.inboxes.length ? <p className={styles.muted}>No accessible inboxes.</p> : query.data.inboxes.map(inbox => <label className={styles.scope} key={inbox.id}><input type="checkbox" checked={pending?.id === inbox.id ? pending.grant : query.data.granted.includes(inbox.id)} disabled={busy || !client.active} onChange={event => void change(inbox.id, event.target.checked)} />{inbox.name}</label>)}
  </div></AccountCard>;
}
