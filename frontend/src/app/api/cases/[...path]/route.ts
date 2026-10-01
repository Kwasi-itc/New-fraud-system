import { caseFetch, failure, relay } from "@/lib/server/case-proxy";

type Context = { params: Promise<{ path: string[] }> };

async function handle(request: Request, context: Context) {
  const { path } = await context.params;
  if (path[0] !== "tenants" || !/^[0-9a-f-]{36}$/i.test(path[1] ?? "") ||
      path.some(part => /[\s/\\?#]/.test(part) || !part || part === "." || part === "..")) {
    return failure("Invalid case endpoint.", 400);
  }
  const url = new URL(request.url);
  const upload = request.method === "PUT" && path.length === 7 && path[2] === "cases" && path[4] === "uploads" && path[6] === "content";
  const download = request.method === "GET" && path.length === 7 && path[2] === "cases" && ((path[4] === "files" && path[6] === "download") || (path[4] === "reports" && path[6] === "export"));
  const contentType = upload ? "application/octet-stream" : "application/json";
  let body: string | ArrayBuffer | undefined;
  if (!["GET", "HEAD"].includes(request.method)) {
    const expectedOrigin = process.env.APP_ORIGIN ?? url.origin;
    if (request.headers.get("origin") !== expectedOrigin) return failure("Invalid request origin.", 403);
    if (!(request.headers.get("content-type") ?? "").startsWith(contentType)) return failure(`Expected ${contentType}.`, 415);
    const reader = request.body?.getReader();
    if (reader) {
      const chunks: Uint8Array[] = []; let size = 0;
      for (;;) {
        const { value, done } = await reader.read(); if (done) break;
        size += value.byteLength;
        if (size > (upload ? 10 : 1) * 1024 * 1024) { await reader.cancel(); return failure("Request is too large.", 413); }
        chunks.push(value);
      }
      body = upload ? new Uint8Array(Buffer.concat(chunks)).buffer : Buffer.concat(chunks).toString("utf8");
    }
  }
  const response = await caseFetch(path.map(encodeURIComponent).join("/") + url.search, { method: request.method, body }, contentType);
  if (download && response.ok) return new Response(response.body, { status: response.status, headers: {
    "Cache-Control": "private, no-store", "X-Content-Type-Options": "nosniff", "Content-Security-Policy": "sandbox",
    "Content-Type": response.headers.get("content-type") ?? "application/octet-stream",
    "Content-Disposition": response.headers.get("content-disposition") ?? "attachment",
  } });
  return relay(response);
}

export { handle as GET, handle as POST, handle as PATCH, handle as PUT, handle as DELETE };
