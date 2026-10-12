import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { authCall, cookieOptions, loginCookie, privateHeaders, rawSession, sessionCookie } from "@/lib/server/auth";
type Context = { params: Promise<{ action: string }> };
const fail = (message: string, status: number) => NextResponse.json({ error: message }, { status, headers: privateHeaders });
function origin() { const value = process.env.APP_ORIGIN; if (!value) throw new Error("Application origin required"); return new URL(value).origin; }
function redirect(path: string) { const response = NextResponse.redirect(new URL(path, origin()), 303); response.headers.set("Cache-Control", "private, no-store"); return response; }
export async function GET(request: Request, context: Context) {
  const { action } = await context.params;
  try {
    if (action === "callback") {
      const url = new URL(request.url), jar = await cookies(); const binding = jar.get(loginCookie)?.value;
      jar.set(loginCookie, "", { ...cookieOptions, maxAge: 0 });
      if (!binding || url.searchParams.has("error")) return redirect("/login?error=denied");
      const response = await authCall("login/complete", { binding, state: url.searchParams.get("state") ?? "", code: url.searchParams.get("code") ?? "" });
      if (!response.ok) return redirect("/login?error=denied");
      const result = await response.json(); jar.set(sessionCookie(), result.session, { ...cookieOptions, maxAge: 8 * 60 * 60 }); return redirect("/my-account");
    }
    if (["members", "sessions", "invitations", "audit"].includes(action)) {
      const session = await rawSession(); if (!session) return fail("Sign in required.", 401);
      const after = new URL(request.url).searchParams.get("after") ?? "00000000-0000-0000-0000-000000000000";
      const response = await authCall(`${action}/list`, { session, after });
      return new Response(await response.text(), { status: response.status, headers: { ...privateHeaders, "Content-Type": "application/json" } });
    }
    return fail("Not found", 404);
  } catch { return fail("Authentication is temporarily unavailable. Please retry.", 503); }
}
export async function POST(request: Request, context: Context) {
  const { action } = await context.params;
  try {
    if (request.headers.get("origin") !== origin()) return fail("Invalid request origin.", 403);
    if (action === "start" || action === "invite-start") {
      let token = "";
      if (action === "invite-start") {
        // A fixed-size body keeps invitation secrets out of URLs and access logs.
        if (!request.headers.get("content-type")?.startsWith("application/x-www-form-urlencoded")) return fail("Invalid invitation.", 400);
        const reader = request.body?.getReader(); if (!reader) return fail("Invalid invitation.", 400);
        const chunks: Uint8Array[] = []; let size = 0;
        for (;;) { const { value, done } = await reader.read(); if (done) break; size += value.byteLength; if (size > 256) { await reader.cancel(); return fail("Invalid invitation.", 400); } chunks.push(value); }
        token = new URLSearchParams(Buffer.concat(chunks).toString("utf8")).get("token") ?? "";
        if (!/^[A-Za-z0-9_-]{43}$/.test(token)) return fail("Invalid invitation.", 400);
      }
      const response = await authCall(action === "start" ? "login/start" : "invitations/start", action === "start" ? {} : { token }); if (!response.ok) return fail("Sign-in or invitation is unavailable. Ask your administrator for a new invitation if needed.", response.status === 403 ? 403 : 503);
      const result = await response.json(); (await cookies()).set(loginCookie, result.binding, { ...cookieOptions, maxAge: 300 });
      const redirectResponse = NextResponse.redirect(result.url, 303); redirectResponse.headers.set("Cache-Control", "private, no-store"); return redirectResponse;
    }
    const session = await rawSession();
    if (action === "logout") {
      if (session) { const response = await authCall("logout", { session }); if (!response.ok) return fail("Logout could not complete. Please retry.", 503); }
      (await cookies()).set(sessionCookie(), "", { ...cookieOptions, maxAge: 0 }); const response = redirect("/login"); response.headers.set("Clear-Site-Data", '"cache"'); return response;
    }
    if (action === "members" || action === "sessions" || action === "invitations") {
      if (!session) return fail("Sign in required.", 401);
      if (!request.headers.get("content-type")?.startsWith("application/json")) return fail("JSON required.", 415);
      const reader = request.body?.getReader(); const chunks: Uint8Array[] = []; let size = 0;
      if (reader) for (;;) { const { value, done } = await reader.read(); if (done) break; size += value.byteLength; if (size > 16384) { await reader.cancel(); return fail("Request too large.", 413); } chunks.push(value); }
      let body;
      try { body = JSON.parse(Buffer.concat(chunks).toString("utf8")); } catch { return fail("Invalid JSON.", 400); }
      if (!body || typeof body !== "object" || Array.isArray(body)) return fail("Invalid request.", 400);
      if (action === "invitations") {
        const response = await authCall("invitations/change", { session, action: body.action, id: body.id, version: body.version, email: body.email, roles: body.roles });
        return new Response(response.status === 204 ? null : await response.text(), { status: response.status, headers: { ...privateHeaders, "Content-Type": "application/json" } });
      }
      if (action === "sessions") {
        const response = await authCall("sessions/revoke", { target: body.target, all: body.all, session });
        if (!response.ok) return new Response(await response.text(), { status: response.status, headers: { ...privateHeaders, "Content-Type": "application/json" } });
        const result = await response.json();
        if (result.signed_out) (await cookies()).set(sessionCookie(), "", { ...cookieOptions, maxAge: 0 });
        return NextResponse.json(result, { headers: { ...privateHeaders, ...(result.signed_out ? { "Clear-Site-Data": '"cache"' } : {}) } });
      }
      const response = await authCall("members/change", { user_id: body.user_id, version: body.version, roles: body.roles, active: body.active, session });
      return new Response(response.status === 204 ? null : await response.text(), { status: response.status, headers: { ...privateHeaders, "Content-Type": "application/json" } });
    }
    return fail("Not found", 404);
  } catch { return fail("Authentication is temporarily unavailable. Please retry.", 503); }
}
