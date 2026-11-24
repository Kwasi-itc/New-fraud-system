"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { accountRequest } from "./account/account-request";
import { AccountCard, AccountModal, AccountNotice, AccountPagination, accountDate, styles } from "./account/account-ui";

type Session = { id: string; created_at: string; expires_at: string; idle_until: string; current: boolean };
type Page = { items: Session[]; next_after: string };
export function IdentitySessions({ tenant, actor }: { tenant: string; actor: string }) {
  const [after, setAfter] = useState(""), [confirmation, setConfirmation] = useState<Session | "all" | null>(null);
  const [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState("");
  const query = useQuery({ queryKey: ["account-sessions", tenant, actor, after], queryFn: ({ signal }) => accountRequest<Page>(`sessions?after=${after || "00000000-0000-0000-0000-000000000000"}`, { signal }), staleTime: 0 });
  async function revoke() {
    if (!confirmation) return;
    setBusy(true); setError("");
    try {
      const result = await accountRequest<{ signed_out: boolean }>("sessions", { method: "POST", body: JSON.stringify(confirmation === "all" ? { all: true } : { target: confirmation.id, all: false }) });
      if (result.signed_out) { window.location.assign("/login"); return; }
      setConfirmation(null); setNotice("Session revoked."); await query.refetch();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Session revocation failed."); }
    finally { setBusy(false); }
  }
  return <AccountCard title="Application sessions" description="Your active sessions in this tenant." action={<button className={styles.button} disabled={busy || query.isFetching} onClick={() => void query.refetch()}><RefreshCw size={15} />Refresh</button>}>
    {!confirmation && error && <div className={styles.cardBody}><AccountNotice>{error}</AccountNotice></div>}
    {notice && <div className={styles.cardBody}><AccountNotice kind="success">{notice}</AccountNotice></div>}
    {query.isPending ? <p className={styles.empty}>Loading sessions…</p> : query.isError ? <div className={styles.cardBody}><AccountNotice>{query.error.message}</AccountNotice></div> : !query.data.items.length ? <p className={styles.empty}>No active sessions on this page.</p> : <div className={styles.tableWrap}><table className={`${styles.table} ${styles.sessions}`}><thead><tr>{["Session", "Started", "Idle access ends", "Expires", "Action"].map(label => <th scope="col" key={label}>{label}</th>)}</tr></thead><tbody>{query.data.items.map(item => <tr key={item.id}>
      <td><div className={styles.row}><span className={styles.cellTitle}>{item.current ? "This browser" : "Application session"}</span>{item.current && <span className={styles.badge}>Current</span>}</div><p className={`${styles.cellSub} ${styles.mono}`}>Session · {item.id.slice(0, 8)}</p></td>
      <td data-label="Started">{accountDate(item.created_at)}</td><td data-label="Idle access ends">{accountDate(item.idle_until)}</td><td data-label="Expires">{accountDate(item.expires_at)}</td><td><button className={styles.link} disabled={busy} onClick={() => { setError(""); setConfirmation(item); }}>{item.current ? "Sign out" : "Revoke"}</button></td>
    </tr>)}</tbody></table></div>}
    {(after || query.data?.next_after) && <AccountPagination after={after} next={query.data?.next_after ?? ""} busy={busy || query.isFetching} onChange={setAfter} label="Sessions page" />}
    <div className={styles.cardFooter}><span>Ending an application session leaves your provider sign-in active.</span><button className={styles.link} style={{ color: "#b42318" }} disabled={busy} onClick={() => { setError(""); setConfirmation("all"); }}>Sign out all sessions</button></div>
    {confirmation && <AccountModal title={confirmation === "all" ? "Sign out all sessions?" : confirmation.current ? "Sign out of this browser?" : "Revoke this session?"} busy={busy} onClose={() => setConfirmation(null)}>
      {error && <AccountNotice>{error}</AccountNotice>}<p className={styles.muted}>{confirmation === "all" ? "Every application session in this tenant will end, including this browser. You’ll need to sign in again to continue." : "This application session will end. Signing in again will be required to continue."}</p>
      <div className={styles.formActions}><button className={styles.button} disabled={busy} onClick={() => setConfirmation(null)}>Cancel</button><button className={`${styles.button} ${styles.danger}`} disabled={busy} onClick={() => void revoke()}>{busy ? "Ending access…" : confirmation === "all" ? "Sign out all sessions" : confirmation.current ? "Sign out" : "Revoke session"}</button></div>
    </AccountModal>}
  </AccountCard>;
}
