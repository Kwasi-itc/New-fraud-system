import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { authCall, cookieOptions, humanAuthCall, loginCookie, manageSignInURL, platformSession, privateHeaders, sessionCookie } from "@/lib/server/auth";
type Context = { params: Promise<{ action: string }> };
const fail = (message: string, status: number) => NextResponse.json({ error: message }, { status, headers: privateHeaders });
function origin() { const value = process.env.APP_ORIGIN; if (!value) throw new Error("Application origin required"); return new URL(value).origin; }
function redirect(path: string) { const response = NextResponse.redirect(new URL(path, origin()), 303); response.headers.set("Cache-Control", "private, no-store"); return response; }
function authErrorMessage(body: string) {
  try {
    const parsed: unknown = JSON.parse(body);
    if (parsed && typeof parsed === "object" && "error" in parsed && typeof parsed.error === "string") {
      return parsed.error.replace(/[\r\n\t]/g, " ").slice(0, 500);
    }
  } catch {}
  return "Auth service returned an unreadable error response.";
}
function forward(response: Response) {
  return new Response(response.status === 204 ? null : response.body, {
    status: response.status,
    headers: { ...privateHeaders, "Content-Type": response.headers.get("content-type") ?? "application/json" },
  });
}
function accountPath(action: string, tenant: string) {
  if (action === "profile") return "account/profile";
  if (action === "sessions") return "account/sessions";
  return `tenants/${encodeURIComponent(tenant)}/${action}`;
}
export async function GET(request: Request, context: Context) {
  const { action } = await context.params;
  try {
    if (action === "callback") {
      const url = new URL(request.url), jar = await cookies(); const binding = jar.get(loginCookie)?.value;
      jar.set(loginCookie, "", { ...cookieOptions, maxAge: 0 });
      const providerErrorPresent = url.searchParams.has("error");
      if (!binding || providerErrorPresent) {
        console.warn("OIDC callback rejected before auth completion", {
          reason: !binding ? "missing_login_binding_cookie" : "provider_returned_error",
          bindingPresent: Boolean(binding),
          providerErrorPresent,
        });
        return redirect("/login?error=denied");
      }
      console.info("Sending OIDC login completion request to auth service");
      const response = await authCall("login/complete", { binding, state: url.searchParams.get("state") ?? "", code: url.searchParams.get("code") ?? "" });
      const requestId = response.headers.get("x-request-id") ?? undefined;
      if (!response.ok) {
        const message = authErrorMessage(await response.text());
        console.error("Auth service rejected OIDC login completion", { status: response.status, requestId, error: message });
        return redirect("/login?error=denied");
      }
      const result = await response.json();
      console.info("Auth service completed OIDC login", { status: response.status, requestId });
      jar.set(sessionCookie(), result.session, { ...cookieOptions, maxAge: 8 * 60 * 60 }); return redirect("/my-account");
    }
    if (["members", "sessions", "invitations", "audit"].includes(action)) {
      const current = await platformSession(); if (!current) return fail("Sign in required.", 401);
      const params = new URL(request.url).searchParams;
      if (!params.has("after")) params.set("after", "00000000-0000-0000-0000-000000000000");
      const extras = action === "members" ? `&query=${encodeURIComponent(params.get("query") ?? "")}&role=${encodeURIComponent(params.get("role") ?? "")}` : "";
      const path = `${accountPath(action, current.session.tenant_id)}?after=${encodeURIComponent(params.get("after")!)}${extras}`;
      return forward(await humanAuthCall(current.access_token, path));
    }
    if (action === "profile") {
      const current = await platformSession(); if (!current) return fail("Sign in required.", 401);
      return forward(await humanAuthCall(current.access_token, accountPath(action, current.session.tenant_id)));
    }
    return fail("Not found", 404);
  } catch (error) {
    if (action === "callback") {
      console.error("OIDC callback processing failed", {
        error: error instanceof Error ? error.message.slice(0, 300) : "unknown error",
      });
    }
    return fail("Authentication is temporarily unavailable. Please retry.", 503);
  }
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
    const current = await platformSession();
    if (action === "manage-sign-in") {
      if (!current) return fail("Sign in required.", 401);
      const profile = await humanAuthCall(current.access_token, "account/profile");
      if (!profile.ok) return fail("Your session has expired. Sign in again.", profile.status === 401 || profile.status === 403 ? 401 : 503);
      const target = manageSignInURL();
      if (!target) return fail("Sign-in management is not configured. Contact your administrator.", 503);
      const response = NextResponse.redirect(target, 303); response.headers.set("Cache-Control", "private, no-store"); return response;
    }
    if (action === "logout") {
      if (current) {
        const response = await humanAuthCall(current.access_token, "account/logout", { method: "POST", body: "{}" });
        if (!response.ok) return fail("Logout could not complete. Please retry.", 503);
      }
      (await cookies()).set(sessionCookie(), "", { ...cookieOptions, maxAge: 0 }); const response = redirect("/login"); response.headers.set("Clear-Site-Data", '"cache"'); return response;
    }
    if (action === "members" || action === "sessions" || action === "invitations" || action === "profile") {
      if (!current) return fail("Sign in required.", 401);
      if (!request.headers.get("content-type")?.startsWith("application/json")) return fail("JSON required.", 415);
      const reader = request.body?.getReader(); const chunks: Uint8Array[] = []; let size = 0;
      if (reader) for (;;) { const { value, done } = await reader.read(); if (done) break; size += value.byteLength; if (size > 16384) { await reader.cancel(); return fail("Request too large.", 413); } chunks.push(value); }
      let body;
      try { body = JSON.parse(Buffer.concat(chunks).toString("utf8")); } catch { return fail("Invalid JSON.", 400); }
      if (!body || typeof body !== "object" || Array.isArray(body)) return fail("Invalid request.", 400);
      if (action === "profile") {
        return forward(await humanAuthCall(current.access_token, "account/profile", {
          method: "PUT", body: JSON.stringify({ name: body.name, version: body.version }),
        }));
      }
      if (action === "invitations") {
        return forward(await humanAuthCall(current.access_token, `tenants/${encodeURIComponent(current.session.tenant_id)}/invitations`, {
          method: "POST", body: JSON.stringify({ action: body.action, id: body.id, version: body.version, email: body.email, roles: body.roles }),
        }));
      }
      if (action === "sessions") {
        const response = await humanAuthCall(current.access_token, "account/sessions/revoke", {
          method: "POST", body: JSON.stringify({ target: body.target, all: body.all }),
        });
        if (!response.ok) return new Response(await response.text(), { status: response.status, headers: { ...privateHeaders, "Content-Type": "application/json" } });
        const result = await response.json();
        if (result.signed_out) (await cookies()).set(sessionCookie(), "", { ...cookieOptions, maxAge: 0 });
        return NextResponse.json(result, { headers: { ...privateHeaders, ...(result.signed_out ? { "Clear-Site-Data": '"cache"' } : {}) } });
      }
      return forward(await humanAuthCall(current.access_token, `tenants/${encodeURIComponent(current.session.tenant_id)}/members`, {
        method: "POST", body: JSON.stringify({ user_id: body.user_id, version: body.version, roles: body.roles, active: body.active }),
      }));
    }
    return fail("Not found", 404);
  } catch { return fail("Authentication is temporarily unavailable. Please retry.", 503); }
}
