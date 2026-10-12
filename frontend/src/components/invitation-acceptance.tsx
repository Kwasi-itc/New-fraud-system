"use client";
import { useEffect, useState } from "react";
export function InvitationAcceptance() {
  const [token, setToken] = useState("");
  useEffect(() => {
    const value = window.location.hash.slice(1);
    window.history.replaceState(null, "", window.location.pathname);
    // The fragment exists only in the browser. Capture it after hydration and
    // erase browser history without persisting the bearer token in storage.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (/^[A-Za-z0-9_-]{43}$/.test(value)) setToken(value);
  }, []);
  if (!token) return <p role="status">Open the full invitation link from your email. If it expired, ask your administrator to resend it.</p>;
  return <form method="post" action="/api/auth/invite-start"><input type="hidden" name="token" value={token} /><button className="rounded bg-blue-800 px-4 py-2 text-white" type="submit">Continue to sign in and accept</button></form>;
}
