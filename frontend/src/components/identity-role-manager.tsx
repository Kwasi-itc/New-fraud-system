"use client";

import { useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { identityRequest, type IdentityRole } from "@/lib/identity-api";
import { AccountCard, AccountFrame, AccountModal, AccountNotice, AccountPagination, styles } from "./account/account-ui";
import { capabilityArea, capabilityAreas } from "./account/account-capabilities";

export function IdentityRoleManager({ capabilities, scope, initialRoleId = "" }: { capabilities: string[]; scope: string; initialRoleId?: string }) {
  const [after, setAfter] = useState(""), [selected, setSelected] = useState(initialRoleId), [deleting, setDeleting] = useState(false);
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const list = useQuery({ queryKey: ["identity-roles", scope, after], queryFn: ({ signal }) => identityRequest<{ roles: IdentityRole[] }>(`roles?after=${encodeURIComponent(after)}`, { signal }) });
  const roles = list.data?.roles.slice(0, 100) ?? [], next = (list.data?.roles.length ?? 0) > 100 ? roles[99].id : "";
  const id = selected || roles[0]?.id || "";
  const detail = useQuery({ queryKey: ["identity-role", scope, id], queryFn: ({ signal }) => identityRequest<IdentityRole>(`roles/${encodeURIComponent(id)}`, { signal }), enabled: !!id });
  const current = detail.data;
  async function remove() {
    if (!current) return; setBusy(true); setError("");
    try { await identityRequest(`roles/${current.id}`, { method: "DELETE", body: JSON.stringify({ version: current.version }) }); setDeleting(false); setSelected(""); await list.refetch(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "Role could not be deleted. Remove assignments and pending invitations first."); }
    finally { setBusy(false); }
  }
  return <AccountFrame title="Roles & capabilities" description="Define what your team can do in this tenant." capabilities={capabilities} action={<Link className={`${styles.button} ${styles.primary}`} href="/administration/roles/new">Create custom role</Link>}>
    {!deleting && error && <AccountNotice>{error}</AccountNotice>}
    <div className={styles.roleGrid}><AccountCard title="Roles">
      {list.isPending ? <p className={styles.empty}>Loading roles…</p> : list.isError ? <div className={styles.cardBody}><AccountNotice>{list.error.message}</AccountNotice><button className={styles.link} onClick={() => void list.refetch()}>Retry roles</button></div> : roles.map(role => <button key={role.id} className={styles.roleChoice} aria-pressed={id === role.id} onClick={() => { setSelected(role.id); setError(""); }}><div><strong>{role.name}</strong><span>{role.builtin ? "Predefined role" : "Custom role"} · {role.capabilities.length} capabilities</span></div></button>)}
      {(after || next) && <AccountPagination after={after} next={next} busy={list.isFetching} onChange={cursor => { setAfter(cursor); setSelected(""); }} label="Roles page" />}
    </AccountCard><div className={styles.stack}>
      {detail.isPending && id ? <p className={styles.empty}>Loading role details…</p> : detail.isError ? <AccountNotice>{detail.error.message}</AccountNotice> : current ? <AccountCard title={current.name} description={current.builtin ? "Predefined · read-only" : "Custom capability bundle"} action={current.builtin ? <span className={`${styles.badge} ${styles.gray}`}>Predefined</span> : <div className={`${styles.row} ${styles.wrap}`}><Link className={styles.button} href={`/administration/roles/${encodeURIComponent(current.id)}/edit`}>Edit role</Link><button className={`${styles.button} ${styles.danger}`} onClick={() => { setError(""); setDeleting(true); }}>Delete</button></div>}>
        <div className={styles.cardBody}><p className={styles.muted}>{current.capabilities.length} explicit capabilities. Each account has one assigned role.</p></div>
        {capabilityAreas.slice(1).map(area => { const names = current.capabilities.filter(name => capabilityArea(name).id === area.id); return names.length ? <details className={styles.capDetails} key={area.id}><summary>{area.name}<span className={`${styles.badge} ${styles.gray}`}>{names.length}</span></summary><div className={styles.capTags}>{names.map(name => <span className={styles.badge} key={name}>{name}</span>)}</div></details> : null; })}
        <div className={styles.cardFooter}><span>Capabilities are assigned explicitly.</span><Link className={styles.link} href="/administration/users">View members</Link></div>
      </AccountCard> : <p className={styles.empty}>Select a role to view its capabilities.</p>}
      <AccountNotice kind="info">Tenant Administrators have every tenant-scoped human capability and exclusively manage role creation and assignment.</AccountNotice>
    </div></div>
    {deleting && current && <AccountModal title="Delete custom role?" busy={busy} onClose={() => setDeleting(false)}>{error && <AccountNotice>{error}</AccountNotice>}<p className={styles.muted}>Delete {current.name}? Assigned members and pending invitations must be reassigned or removed first.</p><div className={styles.formActions}><button className={styles.button} disabled={busy} onClick={() => setDeleting(false)}>Cancel</button><button className={`${styles.button} ${styles.danger}`} disabled={busy} onClick={() => void remove()}>{busy ? "Deleting…" : "Delete role"}</button></div></AccountModal>}
  </AccountFrame>;
}
