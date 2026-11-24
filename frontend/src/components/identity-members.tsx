"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { IdentityRolePicker } from "./identity-role-picker";
import { identityRoleLabels } from "@/lib/identity-roles";
import { accountRequest } from "./account/account-request";
import { AccountCard, AccountModal, AccountNotice, AccountPagination, styles } from "./account/account-ui";

type Member = { user_id: string; name: string; roles: string[]; role_name: string; active: boolean; version: number };
export function IdentityMembers({ tenant, actor }: { tenant: string; actor: string }) {
  const scope = `${tenant}:${actor}`;
  const [after, setAfter] = useState(""), [search, setSearch] = useState(""), [debounced, setDebounced] = useState(""), [filter, setFilter] = useState<string[]>([]);
  const [editing, setEditing] = useState<Member | null>(null), [assigned, setAssigned] = useState<string[]>([]), [active, setActive] = useState(true);
  const [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState("");
  useEffect(() => { const timer = setTimeout(() => setDebounced(search.trim()), 250); return () => clearTimeout(timer); }, [search]);
  const query = useQuery({ queryKey: ["account-members", scope, after, debounced, filter[0] ?? ""], queryFn: ({ signal }) => accountRequest<{ items: Member[]; next_after: string }>(`members?${new URLSearchParams({ after: after || "00000000-0000-0000-0000-000000000000", query: debounced, role: filter[0] ?? "" })}`, { signal }) });
  async function save() {
    if (!editing || assigned.length !== 1) return;
    setBusy(true); setError("");
    try {
      await accountRequest("members", { method: "POST", body: JSON.stringify({ user_id: editing.user_id, version: editing.version, roles: assigned, active }) });
      if (editing.user_id === actor) { window.location.assign("/login"); return; }
      setEditing(null); setNotice("Member updated. Their previous application sessions were revoked."); await query.refetch();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Member could not be updated."); }
    finally { setBusy(false); }
  }
  return <>
    {notice && <AccountNotice kind="success">{notice}</AccountNotice>}
    <AccountCard><div className={styles.toolbar}><div className={`${styles.field} ${styles.search}`}><label className="sr-only" htmlFor="member-search">Search members by name</label><input id="member-search" type="search" className={styles.input} placeholder="Search by name prefix…" maxLength={100} value={search} onChange={event => { setSearch(event.target.value); setAfter(""); }} /></div><div style={{ minWidth: 210, maxWidth: "100%" }}><IdentityRolePicker scope={scope} label="Filter by role" selected={filter} onChange={value => { setFilter(value); setAfter(""); }} /></div></div>
      {query.isPending ? <p className={styles.empty}>Loading members…</p> : query.isError ? <div className={styles.cardBody}><AccountNotice>{query.error.message}</AccountNotice><button className={styles.link} onClick={() => void query.refetch()}>Retry members</button></div> : <div className={styles.tableWrap}><table className={styles.table}><thead><tr>{["Member", "Assigned role", "Status", "Action"].map(label => <th scope="col" key={label}>{label}</th>)}</tr></thead><tbody>{!query.data.items.length ? <tr><td colSpan={4} className={styles.empty}>No members match these filters.</td></tr> : query.data.items.map(member => <tr key={member.user_id}>
        <td><div className={styles.row}><span className={styles.cellTitle}>{member.name}</span>{member.user_id === actor && <span className={`${styles.badge} ${styles.gray}`}>You</span>}</div><p className={`${styles.cellSub} ${styles.mono}`}>{member.user_id}</p></td>
        <td><span className={styles.badge}>{member.role_name || identityRoleLabels[member.roles[0]] || member.roles[0]}</span></td><td><span className={`${styles.badge} ${member.active ? styles.green : styles.gray}`}>{member.active ? "Active" : "Deactivated"}</span></td>
        <td><button className={styles.link} disabled={busy} onClick={() => { setError(""); setEditing(member); setAssigned(member.roles); setActive(member.active); }}>Manage</button></td>
      </tr>)}</tbody></table></div>}
      <AccountPagination after={after} next={query.data?.next_after ?? ""} busy={query.isFetching || busy} onChange={setAfter} label="Members page" />
    </AccountCard>
    <AccountNotice kind="info">Each account has one assigned role. Only Tenant Administrators assign roles, and the final active Tenant Administrator must be retained.</AccountNotice>
    {editing && <AccountModal title={`Manage ${editing.name}`} busy={busy} onClose={() => setEditing(null)}><form onSubmit={event => { event.preventDefault(); void save(); }}>
      {error && <AccountNotice>{error}</AccountNotice>}<div className={styles.stack}>
        <IdentityRolePicker scope={scope} label="Assigned role" selected={assigned} selectedName={assigned[0] === editing.roles[0] ? editing.role_name : undefined} onChange={setAssigned} disabled={busy} />
        <div className={styles.field}><label htmlFor="member-status">Membership status</label><select className={styles.select} id="member-status" value={active ? "active" : "inactive"} disabled={busy} onChange={event => setActive(event.target.value === "active")}><option value="active">Active</option><option value="inactive">Deactivated</option></select></div>
        <p className={styles.hint}>Choosing another role replaces the current assignment. Saving revokes this member’s current application sessions.</p>
      </div><div className={styles.formActions}><button className={styles.button} type="button" disabled={busy} onClick={() => setEditing(null)}>Cancel</button><button className={`${styles.button} ${styles.primary}`} disabled={busy || assigned.length !== 1}>{busy ? "Saving…" : "Save member"}</button></div>
    </form></AccountModal>}
  </>;
}
