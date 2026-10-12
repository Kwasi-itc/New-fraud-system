import { platformSession, privateHeaders } from "@/lib/server/auth";

type Context = { params: Promise<{ path: string[] }> };
function failure(status: number, message: string) {
  return Response.json({ error: { code: "data_model_access", message } }, { status, headers: privateHeaders });
}

async function proxy(request: Request, context: Context) {
  try {
    // This proxy forwards a current user credential, never a shared service token.
    if (request.method !== "GET") {
      const origin = process.env.APP_ORIGIN;
      if (!origin) return failure(503, "Application origin is not configured.");
      if (request.headers.get("origin") !== new URL(origin).origin) return failure(403, "Invalid request origin.");
    }
    const identity = await platformSession();
    if (!identity) return failure(401, "Sign in required.");
    const { path } = await context.params;
    if (!path.length || path.some(segment => !/^[A-Za-z0-9_-]+$/.test(segment))) return failure(400, "Invalid API path.");
    const base = process.env.DATA_MODEL_SERVICE_URL;
    if (!base) return failure(503, "Data Model is not configured.");
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
      headers: { Authorization: `Bearer ${identity.access_token}`, "Content-Type": "application/json", Accept: "application/json" },
      cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(30000),
    });
    if (response.status >= 300 && response.status < 400) return failure(502, "Unexpected service redirect.");
    return new Response(response.status === 204 ? null : response.body, {
      status: response.status, headers: { ...privateHeaders, "Content-Type": "application/json" },
    });
  } catch { return failure(503, "Data Model is temporarily unavailable."); }
}

export const GET = proxy;
export const POST = proxy;
export const PUT = proxy;
export const PATCH = proxy;
export const DELETE = proxy;
