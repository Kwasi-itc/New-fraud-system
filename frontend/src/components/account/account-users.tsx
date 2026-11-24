"use client";
import { useState } from "react";
import { IdentityMembers } from "@/components/identity-members";
import { IdentityInvitations } from "@/components/identity-invitations";
import { IdentityAudit } from "@/components/identity-audit";
import { AccountFrame, styles } from "./account-ui";
export function AccountUsers({ capabilities, tenant, actor }: { capabilities: string[]; tenant: string; actor: string }) {
  const [view, setView] = useState("members"), [creating, setCreating] = useState(false), [audit, setAudit] = useState(false);
  return <AccountFrame title="Users & invitations" description="Manage membership and role assignments in this tenant." capabilities={capabilities} action={<button className={`${styles.button} ${styles.primary}`} onClick={() => setCreating(true)}>Invite member</button>}>
    <div className={styles.segmented} aria-label="Membership view"><button aria-pressed={view === "members"} onClick={() => setView("members")}>Members</button><button aria-pressed={view === "invitations"} onClick={() => setView("invitations")}>Invitations</button></div>
    {view === "members" && <IdentityMembers tenant={tenant} actor={actor} />}
    <IdentityInvitations scope={`${tenant}:${actor}`} creating={creating} onClose={() => setCreating(false)} onCreated={() => setView("invitations")} showList={view === "invitations"} />
    {capabilities.includes("audit:read") && <details style={{ marginTop: 24 }} onToggle={event => setAudit(event.currentTarget.open)}><summary className={styles.link}>Access audit trail</summary>{audit && <div style={{ marginTop: 20 }}><IdentityAudit /></div>}</details>}
  </AccountFrame>;
}
