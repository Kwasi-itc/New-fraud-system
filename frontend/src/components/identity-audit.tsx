"use client";
import { useEffect, useState } from "react";
type Event = { id: string; actor: string; action: string; target: string; at: string; details: Record<string, unknown> };
export function IdentityAudit() {
  const [items, setItems] = useState<Event[]>([]), [after, setAfter] = useState(""), [next, setNext] = useState("");
  const [error, setError] = useState(""), [loading, setLoading] = useState(true), [revision, setRevision] = useState(0);
  useEffect(() => {
    const abort = new AbortController();
    fetch(`/api/auth/audit?after=${after || "00000000-0000-0000-0000-000000000000"}`, { cache: "no-store", signal: abort.signal }).then(async r => { if (!r.ok) throw new Error("Audit events could not be loaded."); return r.json(); }).then(data => { setItems(data.items); setNext(data.next_after); }).catch(e => { if (!abort.signal.aborted) setError(e.message); }).finally(() => { if (!abort.signal.aborted) setLoading(false); });
    return () => abort.abort();
  }, [after, revision]);
  function refresh(cursor = after) { setError(""); setLoading(true); setAfter(cursor); setRevision(v => v + 1); }
  return <section className="space-y-3"><h2 className="text-xl font-semibold">Access audit trail</h2>{error && <p role="alert">{error}</p>}<button className="underline" disabled={loading} onClick={() => refresh()}>Refresh audit trail</button>{loading ? <p>Loading audit events…</p> : <ul className="space-y-3">{items.map(e => <li key={e.id} className="rounded border p-3"><p>{e.action} — {e.at}</p><p>Actor: {e.actor}</p><p>Target: {e.target}</p><details><summary>Change details</summary><pre className="overflow-auto whitespace-pre-wrap">{JSON.stringify(e.details, null, 2)}</pre></details></li>)}</ul>}<div className="flex gap-4"><button disabled={loading || !after} onClick={() => refresh("")}>First audit page</button><button disabled={loading || !next} onClick={() => refresh(next)}>Next audit page</button></div></section>;
}
