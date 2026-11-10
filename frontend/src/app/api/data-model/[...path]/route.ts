import { platformSession, privateHeaders } from "@/lib/server/auth";
import { localMySQLSession } from "@/lib/server/local-mysql-session";
import { ownsDataModelResource } from "@/lib/server/data-model-resource";
import { serviceSecret } from "@/lib/server/service-secret";
import { httpTest } from "../../../../../scripts/http-test.mjs";

type Context = { params: Promise<{ path: string[] }> };
function failure(status: number, message: string) {
  return Response.json({ error: { code: "data_model_access", message } }, { status, headers: privateHeaders });
}

async function proxy(request: Request, context: Context) {
  try {
    // The gateway checks the current user and tenant before forwarding. The
    // explicit EC2 test profile uses its private data-model service credential.
    if (request.method !== "GET") {
      const origin = process.env.APP_ORIGIN;
      if (!origin) return failure(503, "Application origin is not configured.");
      if (request.headers.get("origin") !== new URL(origin).origin) return failure(403, "Invalid request origin.");
    }
    const identity = await platformSession();
    if (!identity) return failure(401, "Sign in required.");
    const { path } = await context.params;
    if (!path.length || path.some(segment => !/^[A-Za-z0-9_-]+$/.test(segment))) return failure(400, "Invalid API path.");
    if (!identity.session.roles.some(role => ["platform_viewer", "platform_builder", "platform_publisher", "platform_admin"].includes(role))) return failure(403, "Platform access required.");
    if (path[0] === "tenants" && path[1] !== identity.session.tenant_id) return failure(403, "This workspace belongs to another tenant.");
    const read = request.method === "GET" || (request.method === "POST" && path.length === 3 && path[2] === "record-displays");
    if (!read && !identity.session.roles.some(role => ["platform_builder", "platform_admin"].includes(role))) return failure(403, "Builder access required.");
    if (localMySQLSession() && path[0] === "tenants" && path[1] !== identity.session.tenant_id) return failure(403, "Local MySQL is scoped to the configured tenant.");
    const base = process.env.DATA_MODEL_SERVICE_URL;
    if (!base) return failure(503, "Data Model is not configured.");
    const backendCredential = httpTest() ? serviceSecret("DATA_MODEL_AUTH_TOKEN") : identity.access_token;
    if (!backendCredential) return failure(503, "Data Model credential is not configured.");
    if (path[0] !== "tenants") {
      const owner = await fetch(`${base.replace(/\/$/, "")}/v1/tenants/${encodeURIComponent(identity.session.tenant_id)}/data-model`, {
        headers: { Authorization: `Bearer ${backendCredential}` }, cache: "no-store", redirect: "error", signal: AbortSignal.timeout(15000),
      });
      if (!owner.ok) return failure(503, "Resource ownership is unavailable.");
      const data = await owner.json();
      if (!data.data_model || !ownsDataModelResource(data.data_model, path)) return failure(403, "Resource access denied.");
    }
    const headers = new Headers({ Authorization: `Bearer ${backendCredential}`, "Content-Type": "application/json", Accept: "application/json" });
    const key = request.headers.get("idempotency-key");
    if (key) headers.set("Idempotency-Key", key);
    let body: ArrayBuffer | undefined;
    if (request.method !== "GET") {
      const chunks: Uint8Array[] = [];
      let length = 0;
      const reader = request.body?.getReader();
      if (reader) for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        length += value.byteLength;
        if (length > 4 * 1024 * 1024) { await reader.cancel(); return failure(413, "Request is too large."); }
        chunks.push(value);
      }
      body = new Uint8Array(Buffer.concat(chunks)).buffer;
    }
    const response = await fetch(`${base.replace(/\/$/, "")}/v1/${path.join("/")}${new URL(request.url).search}`, {
      method: request.method, body,
      headers,
      cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(30000),
    });
    if (response.status >= 300 && response.status < 400) return failure(502, "Unexpected service redirect.");
    const outgoing = new Headers({ ...privateHeaders, "Content-Type": "application/json" });
    const operation = response.headers.get("idempotency-key");
    if (operation) outgoing.set("Idempotency-Key", operation);
    return new Response(response.status === 204 ? null : response.body, {
      status: response.status, headers: outgoing,
    });
  } catch { return failure(503, "Data Model is temporarily unavailable."); }
}

export const GET = proxy;
export const POST = proxy;
export const PUT = proxy;
export const PATCH = proxy;
export const DELETE = proxy;
