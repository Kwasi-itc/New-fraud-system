import "server-only";
import { platformSession, privateHeaders, rawSession } from "./auth";
import { serviceSecret } from "./service-secret";

type Service = "ingestion" | "decisions";
const roles = ["platform_viewer", "platform_builder", "platform_publisher", "platform_admin"];
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const failure = (status: number, message: string) => Response.json({ error: { code: "platform_access", message } }, { status, headers: privateHeaders });

export async function platformProxy(request: Request, path: string[], service: Service) {
  try {
    if (!path.length || path.some(segment => !segment || segment === "." || segment === ".." || /[/\\?#\x00-\x1f]/.test(segment))) return failure(400, "Invalid API path.");
    const write = request.method !== "GET";
    if (write) {
      const origin = process.env.APP_ORIGIN;
      if (!origin) return failure(503, "Application origin is not configured.");
      if (request.headers.get("origin") !== new URL(origin).origin) return failure(403, "Invalid request origin.");
    }
    const identity = await platformSession();
    if (!identity) return failure(401, "Sign in required.");
    const { session } = identity;
    if (!session.roles.some(role => roles.includes(role))) return failure(403, "Platform access required.");
    const tenantPath = path[0] === "tenants" && uuid.test(path[1] ?? "");
    const catalog = service === "decisions" && request.method === "GET" && path.length === 1 && path[0] === "rule-functions";
    const uploadLog = service === "ingestion" && request.method === "GET" && path.length === 2 && path[0] === "upload-logs" && uuid.test(path[1]);
    if (!tenantPath && !catalog && !uploadLog) return failure(400, "A tenant-scoped endpoint is required.");
    if (tenantPath && path[1] !== session.tenant_id) return failure(403, "This workspace belongs to another tenant.");
    // Private execution recovery and raw screening administration have separate
    // identity-aware gateways. They are never exposed by this general proxy.
    if (path.some(part => ["screening-executions", "workflow-rules", "workflows", "screening-configs", "outbox-events"].includes(part))) return failure(403, "Use the authorized workspace action.");
    const publish = path.some(part => ["commit", "deactivate", "publications"].includes(part));
    if (write && !session.roles.some(role => [publish ? "platform_publisher" : "platform_builder", "platform_admin"].includes(role))) return failure(403, publish ? "Publisher access required." : "Builder access required.");
    const base = process.env[service === "ingestion" ? "INGESTION_SERVICE_URL" : "DECISION_ENGINE_SERVICE_URL"];
    const token = service === "decisions" && process.env.BACKEND_PROFILE === "mysql"
      ? await rawSession()
      : serviceSecret(service === "ingestion" ? "INGESTION_AUTH_TOKEN" : "DECISION_ENGINE_AUTH_TOKEN");
    if (!base || !token) return failure(503, "The backend service is not configured.");
    const headers = new Headers({ Accept: "application/json", Authorization: `Bearer ${token}` });
    const contentType = request.headers.get("content-type");
    if (contentType) headers.set("Content-Type", contentType);
    const key = request.headers.get("idempotency-key");
    if (key) headers.set("Idempotency-Key", key);
    let body: Uint8Array<ArrayBuffer> | undefined;
    if (write) {
      const chunks: Uint8Array[] = []; let size = 0;
      const reader = request.body?.getReader();
      if (reader) for (;;) {
        const { value, done } = await reader.read(); if (done) break;
        size += value.byteLength;
        if (size > (service === "ingestion" ? 16 : 4) * 1024 * 1024) { await reader.cancel(); return failure(413, "Request is too large."); }
        chunks.push(value);
      }
      body = new Uint8Array(Buffer.concat(chunks));
    }
    const response = await fetch(`${base.replace(/\/$/, "")}/v1/${path.map(encodeURIComponent).join("/")}${new URL(request.url).search}`, {
      method: request.method, headers, body, cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(30000),
    });
    if (response.status >= 300 && response.status < 400) return failure(502, "Unexpected service redirect.");
    if (uploadLog && response.ok) {
      const data = await response.json();
      if (data.upload_log?.tenant_id !== session.tenant_id) return failure(403, "Upload access denied.");
      return Response.json(data, { headers: privateHeaders });
    }
    const outgoing = new Headers({ ...privateHeaders, "Content-Type": response.headers.get("content-type") ?? "application/json" });
    for (const name of ["retry-after", "idempotency-key"]) { const value = response.headers.get(name); if (value) outgoing.set(name, value); }
    return new Response(response.status === 204 ? null : response.body, { status: response.status, headers: outgoing });
  } catch { return failure(503, "The backend service is temporarily unavailable."); }
}
