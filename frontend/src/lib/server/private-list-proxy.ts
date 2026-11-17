import "server-only";
import { platformSession } from "./auth";
import { failure, relay, privateHeaders } from "./case-proxy";

export async function privateListProxy(request: Request, path: string[] = []) {
  const [list, imports, id, action] = path;
  const valid = path.length === 0 || (path.length === 1 && (list === "capabilities" || /^private-[a-f0-9]{32}$/.test(list))) ||
    (/^private-[a-f0-9]{32}$/.test(list ?? "") && ((path.length === 2 && ["imports", "recover"].includes(imports)) ||
      (imports === "imports" && /^[0-9a-f-]{36}$/i.test(id ?? "") && (path.length === 3 || (path.length === 4 && ["diff", "artifact", "approve", "reject"].includes(action))))));
  if (!valid) return failure("Invalid private-list request.", 400);
  let body: string | undefined;
  if (request.method === "POST") {
    if (request.headers.get("origin") !== (process.env.APP_ORIGIN ?? new URL(request.url).origin)) return failure("Invalid request origin.", 403);
    if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) return failure("Expected application/json.", 415);
    const reader = request.body?.getReader(); const chunks: Uint8Array[] = []; let size = 0;
    if (reader) for (;;) {
      const { value, done } = await reader.read(); if (done) break;
      size += value.byteLength;
      if (size > 2 * 5 * 1024 * 1024 + 8192) { await reader.cancel(); return failure("Upload is too large.", 413); }
      chunks.push(value);
    }
    body = Buffer.concat(chunks).toString("utf8");
  }
  try {
    const session = await platformSession();
    if (!session) return failure("Sign in to manage private lists.", 401);
    const base = process.env.SCREENING_SERVICE_URL;
    if (!base) return failure("Screening is not configured.", 503);
    const query = request.method === "GET" ? new URL(request.url).search : "";
    const result = await fetch(`${base.replace(/\/$/, "")}/v1/private-lists${path.length ? `/${path.join("/")}` : ""}${query}`, {
      method: request.method, body, headers: { Authorization: `Bearer ${session.access_token}`, "Content-Type": "application/json" },
      cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(60000),
    });
    if (action === "artifact" && result.ok) return new Response(await result.arrayBuffer(), { status: 200, headers: { ...privateHeaders, "Content-Type": "text/plain; charset=utf-8", "Content-Disposition": 'attachment; filename="original-upload.txt"', "X-Content-Type-Options": "nosniff", "X-Artifact-SHA256": result.headers.get("X-Artifact-SHA256") ?? "" } });
    return await relay(result);
  } catch { return failure("Private-list response unavailable. Refresh to check the same operation before retrying.", 503); }
}
