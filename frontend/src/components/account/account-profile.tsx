"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { LockKeyhole, LogOut } from "lucide-react";
import type { AccountProfile } from "@/lib/server/auth";
import { IdentitySessions } from "@/components/identity-sessions";
import { AccountCard, AccountFrame, AccountModal, AccountNotice, styles } from "./account-ui";

export function AccountProfilePage({ profile, capabilities, canManageSignIn }: { profile: AccountProfile; capabilities: string[]; canManageSignIn: boolean }) {
  const router = useRouter();
  const [editing, setEditing] = useState(false), [references, setReferences] = useState(false), [name, setName] = useState(profile.name);
  const [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState("");
  const initials = profile.name.trim().split(/\s+/).slice(0, 2).map(part => part[0]).join("").toUpperCase() || "U";
  async function save() {
    setBusy(true); setError("");
    try {
      const response = await fetch("/api/auth/profile", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: name.trim(), version: profile.version }) });
      if (!response.ok) throw new Error(response.status === 409 ? "Your profile changed. Refresh the page before saving again." : "Your profile could not be saved. Refresh and retry.");
      setEditing(false); setNotice("Profile updated."); router.refresh();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Profile update failed."); }
    finally { setBusy(false); }
  }
  return <AccountFrame title="My account" description="Manage your profile, sign-in, and sessions." capabilities={capabilities} action={<form action="/api/auth/logout" method="post"><button className={styles.button}><LogOut size={16} />Sign out</button></form>}>
    {notice && <AccountNotice kind="success">{notice}</AccountNotice>}
    <div className={styles.stack}>
      <AccountCard><div className={styles.profileHeader}>
        <div className={styles.row}><div className={styles.avatar}>{initials}</div><div><h2 className={styles.profileName}>{profile.name}</h2><span className={styles.badge}>{profile.role_name}</span></div></div>
        <div className={styles.row}><button className={styles.button} onClick={() => { setName(profile.name); setError(""); setEditing(true); }}>Edit profile</button><form action="/api/auth/manage-sign-in" method="post"><button className={styles.button} disabled={!canManageSignIn} title={!canManageSignIn ? "Sign-in management is not configured. Contact your administrator." : undefined}><LockKeyhole size={16} />Manage sign-in</button></form></div>
      </div><div className={styles.profileFacts}>
        {[ ["Account name", profile.account_name || "Not shared by your provider"], ["Email address", profile.email || "Not shared by your provider"], ["Tenant", profile.tenant_name] ].map(([label, value]) => <div key={label}><p className={styles.factLabel}>{label}</p><p className={styles.factValue}>{value}</p></div>)}
      </div><div className={styles.cardFooter}><span>Account ID <code className={styles.mono}>{profile.user_id.slice(0, 8)}…{profile.user_id.slice(-4)}</code></span><button className={styles.link} onClick={() => setReferences(true)}>View account IDs</button></div></AccountCard>
      <IdentitySessions tenant={profile.tenant_id} actor={profile.user_id} />
    </div>
    {editing && <AccountModal title="Edit profile" busy={busy} onClose={() => setEditing(false)}><form onSubmit={event => { event.preventDefault(); void save(); }}>
      {error && <AccountNotice>{error}</AccountNotice>}<div className={styles.field}><label htmlFor="display-name">Display name</label><input id="display-name" className={styles.input} required maxLength={200} value={name} disabled={busy} autoComplete="name" onChange={event => setName(event.target.value)} /><p className={styles.hint}>Shown to colleagues across your account. Your sign-in provider manages your account name, email, and credentials.</p></div>
      <div className={styles.formActions}><button className={styles.button} type="button" disabled={busy} onClick={() => setEditing(false)}>Cancel</button><button className={`${styles.button} ${styles.primary}`} disabled={busy || !name.trim()}>{busy ? "Saving…" : "Save changes"}</button></div>
    </form></AccountModal>}
    {references && <AccountModal title="Account references" onClose={() => setReferences(false)}><div className={styles.stack}>{[["Account ID", profile.user_id], ["Tenant ID", profile.tenant_id]].map(([label, value]) => <div key={label}><p className={styles.factLabel}>{label}</p><code className={styles.mono}>{value}</code></div>)}</div></AccountModal>}
  </AccountFrame>;
}
