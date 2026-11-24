import { platformSession, privateHeaders } from "@/lib/server/auth";
type Context = { params: Promise<{ path: string[] }> };
const routes: Array<[RegExp, string[]]> = [
  [/^capabilities$/, ["GET"]], [/^client-scopes$/, ["GET"]],
  [/^roles$/, ["GET", "POST"]], [/^roles\/role_[0-9a-f-]{36}$/, ["PUT", "DELETE"]],
  [/^roles\/(?:role_[0-9a-f-]{36}|tenant_admin|case_manager_admin|case_investigator|platform_viewer|platform_builder|platform_publisher|platform_admin)$/, ["GET"]],
  [/^clients$/, ["GET", "POST"]], [/^clients\/[0-9a-f-]{36}$/, ["GET", "PUT"]],
  [/^client-keys$/, ["GET", "POST"]], [/^client-keys\/[^/]{1,100}$/, ["DELETE"]],
  [/^clients\/[0-9a-f-]{36}\/scope-approvals$/, ["GET"]],
  [/^clients\/[0-9a-f-]{36}\/scope-approvals\/[a-z-]+:[a-z-]+$/, ["POST"]],
];
const fail = (status: number, error: string) => Response.json({ error }, { status, headers: privateHeaders });
async function proxy(request: Request, context: Context) {
  try {
    const { path } = await context.params;
    if (path.some(part => !part || part === "." || part === ".." || /[/\\?#\x00-\x1f]/.test(part))) return fail(400, "Invalid identity path.");
    const value = path.join("/");
    if (!routes.some(([pattern, methods]) => pattern.test(value) && methods.includes(request.method))) return fail(404, "Identity operation not found.");
    if (request.method !== "GET") {
      const origin = process.env.APP_ORIGIN;
      if (!origin || request.headers.get("origin") !== new URL(origin).origin) return fail(403, "Invalid request origin.");
    }
    const current = await platformSession();
    if (!current) return fail(401, "Sign in required.");
    const base = process.env.AUTH_SERVICE_URL;
    if (!base) return fail(503, "Identity service is not configured.");
    let body: Uint8Array<ArrayBuffer> | undefined;
    if (request.method !== "GET") {
      const reader = request.body?.getReader(), chunks: Uint8Array[] = []; let size = 0;
      if (reader) for (;;) { const { value, done } = await reader.read(); if (done) break; size += value.byteLength; if (size > 16384) { await reader.cancel(); return fail(413, "Request too large."); } chunks.push(value); }
      body = new Uint8Array(Buffer.concat(chunks));
    }
    const response = await fetch(`${base.replace(/\/$/, "")}/v1/tenants/${encodeURIComponent(current.session.tenant_id)}/${path.map(encodeURIComponent).join("/")}${new URL(request.url).search}`, {
      method: request.method, body, headers: { Authorization: `Bearer ${current.access_token}`, "Content-Type": "application/json" },
      cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(15000),
    });
    if (response.status >= 300 && response.status < 400) return fail(502, "Unexpected identity redirect.");
    return new Response(response.status === 204 ? null : response.body, { status: response.status, headers: { ...privateHeaders, "Content-Type": response.headers.get("content-type") ?? "application/json" } });
  } catch { return fail(503, "Identity service is temporarily unavailable."); }
}
export const GET = proxy; export const POST = proxy; export const PUT = proxy; export const DELETE = proxy;

