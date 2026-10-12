"use client";

import { useEffect, useState } from "react";

type Session = { id: string; created_at: string; expires_at: string; idle_until: string; current: boolean };

export function IdentitySessions() {
  const [items, setItems] = useState<Session[]>([]);
  const [after, setAfter] = useState(""), [next, setNext] = useState("");
  const [loading, setLoading] = useState(true), [busy, setBusy] = useState(false);
  const [error, setError] = useState(""), [notice, setNotice] = useState("");
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    fetch(`/api/auth/sessions?after=${encodeURIComponent(after || "00000000-0000-0000-0000-000000000000")}`, { cache: "no-store", signal: controller.signal })
      .then(async response => {
        if (!response.ok) throw new Error("Sessions could not be loaded. Your session may have expired; sign in again or retry.");
        return response.json();
      })
      .then(data => { if (!controller.signal.aborted) { setItems(data.items); setNext(data.next_after); } })
      .catch(reason => { if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "Sessions unavailable."); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [after, revision]);

  function refresh(cursor = after) { setLoading(true); setError(""); setAfter(cursor); setRevision(value => value + 1); }
  async function revoke(target?: string) {
    if (!window.confirm(target ? "Revoke this application session? Its access will end immediately." : "Sign out all of your application sessions in this tenant, including this browser?")) return;
    setBusy(true); setError(""); setNotice("");
    try {
      const response = await fetch("/api/auth/sessions", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(target ? { target, all: false } : { all: true }) });
      if (!response.ok) throw new Error("Session revocation could not be confirmed. Refresh and retry; you may need to sign in again.");
      const result = await response.json();
      if (result.signed_out) { window.location.assign("/login"); return; }
      setNotice("Session revoked."); refresh("");
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Revocation failed."); }
    finally { setBusy(false); }
  }

  return <section aria-labelledby="sessions-title" className="space-y-4">
    <h2 id="sessions-title" className="text-xl font-semibold">Your application sessions</h2>
    <p>Only your sessions in the current tenant are shown. Revocation ends application access; it does not sign you out of your identity provider.</p>
    {error && <p role="alert" className="text-red-700">{error}</p>}
    {notice && <p role="status">{notice}</p>}
    {loading ? <p>Loading sessions…</p> : !error && (items.length === 0 ? <p>No active sessions on this page.</p> : <ul className="space-y-3">{items.map(item => <li key={item.id} className="rounded border p-4">
      <p className="font-medium">{item.current ? "This browser session" : "Other session"}</p>
      <p>Started: {new Date(item.created_at).toLocaleString()}</p>
      <p>Expires: {new Date(item.expires_at).toLocaleString()}</p>
      <p>Idle expiry: {new Date(item.idle_until).toLocaleString()}</p>
      <button disabled={busy} className="underline" onClick={() => revoke(item.id)}>{item.current ? "Revoke this session" : "Revoke session"}</button>
    </li>)}</ul>)}
    <div className="flex flex-wrap gap-4">
      <button disabled={busy || loading} onClick={() => refresh()}>Refresh sessions</button>
      <button disabled={busy || loading || !after} onClick={() => refresh("")}>First sessions page</button>
      <button disabled={busy || loading || !next || !!error} onClick={() => refresh(next)}>Next sessions page</button>
      <button disabled={busy || loading} className="rounded border px-4 py-2" onClick={() => revoke()}>Sign out all sessions in this tenant</button>
    </div>
  </section>;
}
