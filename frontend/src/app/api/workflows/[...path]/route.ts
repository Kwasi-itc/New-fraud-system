import { platformSession, privateHeaders } from "@/lib/server/auth";

type Context = { params: Promise<{ path: string[] }> };
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
function failure(status: number, error: string) { return Response.json({ error }, { status, headers: privateHeaders }); }
async function proxy(request: Request, context: Context) {
  try {
    if (request.method !== "GET") {
      const origin = process.env.APP_ORIGIN;
      if (!origin) return failure(503, "Application origin is not configured.");
      if (request.headers.get("origin") !== new URL(origin).origin) return failure(403, "Invalid request origin.");
    }
    const identity = await platformSession();
    if (!identity) return failure(401, "Sign in required.");
    const { session } = identity;
    const canEdit = session.roles.includes("platform_admin");
    if (!session.roles.some(r => ["platform_viewer", "platform_builder", "platform_publisher", "platform_admin"].includes(r))) return failure(403, "Platform access required.");
    if (request.method !== "GET" && !canEdit) return failure(403, "A platform administrator must save live workflow changes.");
    const { path } = await context.params;
    if (path[0] !== "scenarios" || !uuid.test(path[1] ?? "") ||
      (request.method === "GET" ? path.length !== 2 : path.length !== 4 || path[2] !== "rules" || !uuid.test(path[3]))) return failure(400, "Invalid workflow path.");
    const base = process.env.DECISION_ENGINE_SERVICE_URL, token = process.env.WORKFLOW_AUTHORING_TOKEN;
    if (!base || !token || token.length < 32) return failure(503, "Workflow authoring is not configured.");
    let body: Uint8Array<ArrayBuffer> | undefined;
    if (request.method !== "GET") {
      const chunks: Uint8Array[] = []; let length = 0;
      const reader = request.body?.getReader();
      if (reader) for (;;) {
        const { value, done } = await reader.read(); if (done) break;
        length += value.byteLength;
        if (length > 16384) { await reader.cancel(); return failure(413, "Workflow configuration is too large."); }
        chunks.push(value);
      }
      body = new Uint8Array(Buffer.concat(chunks));
    }
    const response = await fetch(`${base.replace(/\/$/, "")}/internal/workflow-authoring/tenants/${encodeURIComponent(session.tenant_id)}/${path.join("/")}`, {
      method: request.method, body,
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json", "X-Workflow-User": session.user_id, "X-Workflow-Session": session.id, "X-Workflow-Version": String(session.version) },
      cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(15000),
    });
    if (response.status >= 300 && response.status < 400) return failure(502, "Unexpected service redirect.");
    const data = await response.json();
    return Response.json(response.ok && request.method === "GET" ? { ...data, tenant_id: session.tenant_id, can_edit: canEdit } : data, { status: response.status, headers: privateHeaders });
  } catch { return failure(503, "Workflow service is unavailable. Refresh before retrying."); }
}
export const GET = proxy;
export const PUT = proxy;
