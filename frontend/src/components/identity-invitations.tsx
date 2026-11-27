"use client";

import { createUuid } from "@/lib/create-uuid";

import { useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { identityRoleLabels } from "@/lib/identity-roles";
import { IdentityRolePicker } from "./identity-role-picker";
import { accountRequest } from "./account/account-request";
import { AccountCard, AccountModal, AccountNotice, AccountPagination, accountDate, styles } from "./account/account-ui";

type Invitation = { id: string; email: string; roles: string[]; status: string; delivery: string; expires_at: string; version: number };
export function IdentityInvitations({ creating, onClose, onCreated, showList, scope }: { creating: boolean; onClose: () => void; onCreated: () => void; showList: boolean; scope: string }) {
  const [after, setAfter] = useState(""), [email, setEmail] = useState(""), [roles, setRoles] = useState(["case_investigator"]);
  const [confirmation, setConfirmation] = useState<{ item: Invitation; action: "cancel" | "resend" } | null>(null);
  const [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState("");
  const request = useRef<{ key: string; id: string } | null>(null), client = useQueryClient();
  const query = useQuery({ queryKey: ["account-invitations", scope, after], queryFn: ({ signal }) => accountRequest<{ items: Invitation[]; next_after: string }>(`invitations?after=${after || "00000000-0000-0000-0000-000000000000"}`, { signal }), enabled: showList });
  async function mutate(action: "create" | "cancel" | "resend", item?: Invitation) {
    setBusy(true); setError(""); setNotice("");
    const key = JSON.stringify([email.trim(), roles]);
    try {
      if (!request.current || request.current.key !== key) request.current = { key, id: createUuid() };
      await accountRequest("invitations", { method: "POST", body: JSON.stringify(action === "create" ? { action, id: request.current.id, email: email.trim(), roles } : { action, id: item!.id, version: item!.version }) });
      setNotice(action === "cancel" ? "Invitation revoked." : "Invitation queued for email delivery. Check its delivery status below.");
      setConfirmation(null); setAfter("");
      if (action === "create") { setEmail(""); request.current = null; onClose(); onCreated(); }
      await client.invalidateQueries({ queryKey: ["account-invitations", scope] });
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Invitation update failed."); }
    finally { setBusy(false); }
  }
  return <>
    {showList && <>{notice && <AccountNotice kind="success">{notice}</AccountNotice>}{error && !creating && !confirmation && <AccountNotice>{error}</AccountNotice>}
      <AccountCard title="Invitations" action={<button className={styles.button} disabled={query.isFetching || busy} onClick={() => void query.refetch()}>Refresh</button>}>
        {query.isPending ? <p className={styles.empty}>Loading invitations…</p> : query.isError ? <div className={styles.cardBody}><AccountNotice>{query.error.message}</AccountNotice></div> : <div className={styles.tableWrap}><table className={styles.table}><thead><tr>{["Email", "Assigned role", "Status", "Delivery", "Action"].map(label => <th scope="col" key={label}>{label}</th>)}</tr></thead><tbody>{!query.data?.items.length ? <tr><td className={styles.empty} colSpan={5}>No invitations on this page.</td></tr> : query.data.items.map(item => <tr key={item.id}>
          <td><p className={styles.cellTitle}>{item.email}</p><p className={styles.cellSub}>Expires {accountDate(item.expires_at)}</p></td><td><span className={styles.badge}>{identityRoleLabels[item.roles[0]] ?? item.roles[0]}</span></td><td>{item.status}</td><td>{item.delivery === "sent" ? "Accepted by mail server" : item.delivery}</td>
          <td>{["pending", "expired"].includes(item.status) && <div className={`${styles.row} ${styles.wrap}`}><button className={styles.link} disabled={busy} onClick={() => { setError(""); setConfirmation({ item, action: "resend" }); }}>Resend</button><button className={styles.link} disabled={busy} onClick={() => { setError(""); setConfirmation({ item, action: "cancel" }); }}>Revoke invitation</button></div>}</td>
        </tr>)}</tbody></table></div>}
        <AccountPagination after={after} next={query.data?.next_after ?? ""} busy={busy || query.isFetching} onChange={setAfter} label="Invitations page" />
      </AccountCard>
    </>}
    {creating && <AccountModal title="Invite a team member" busy={busy} onClose={onClose}><form onSubmit={event => { event.preventDefault(); void mutate("create"); }}>{error && <AccountNotice>{error}</AccountNotice>}<div className={styles.stack}><div className={styles.field}><label htmlFor="invite-email">Email address</label><input className={styles.input} id="invite-email" type="email" required maxLength={254} value={email} disabled={busy} onChange={event => setEmail(event.target.value)} /></div><IdentityRolePicker scope={scope} label="Role" disabled={busy} selected={roles} onChange={setRoles} /><p className={styles.hint}>The selected role determines this account’s access. A verified identity-provider email is required to accept the invitation.</p></div><div className={styles.formActions}><button type="button" className={styles.button} disabled={busy} onClick={onClose}>Cancel</button><button className={`${styles.button} ${styles.primary}`} disabled={busy || roles.length !== 1}>{busy ? "Queueing…" : "Invite member"}</button></div></form></AccountModal>}
    {confirmation && <AccountModal title={confirmation.action === "cancel" ? "Revoke this invitation?" : "Resend this invitation?"} busy={busy} onClose={() => setConfirmation(null)}>{error && <AccountNotice>{error}</AccountNotice>}<p className={styles.muted}>{confirmation.item.email}: previous invitation links will stop working.</p><div className={styles.formActions}><button className={styles.button} disabled={busy} onClick={() => setConfirmation(null)}>Cancel</button><button className={`${styles.button} ${confirmation.action === "cancel" ? styles.danger : styles.primary}`} disabled={busy} onClick={() => void mutate(confirmation.action, confirmation.item)}>{busy ? "Updating…" : confirmation.action === "cancel" ? "Revoke invitation" : "Resend invitation"}</button></div></AccountModal>}
  </>;
}
