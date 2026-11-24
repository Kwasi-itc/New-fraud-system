import { privateHeaders } from "@/lib/server/auth";

// Public client-credentials exchange. The auth owner validates the signed
// assertion; browser cookies and BFF service credentials grant no authority here.
export async function POST(request: Request) {
  const fail = (status: number, error: string) => Response.json({ error }, { status, headers: privateHeaders });
  const base = process.env.AUTH_SERVICE_URL;
  if (!base) return fail(503, "temporarily_unavailable");
  if (!request.headers.get("content-type")?.startsWith("application/x-www-form-urlencoded")) return fail(415, "invalid_request");
  try {
    const reader = request.body?.getReader(), chunks: Uint8Array[] = []; let size = 0;
    if (reader) for (;;) { const { value, done } = await reader.read(); if (done) break; size += value.byteLength; if (size > 32768) { await reader.cancel(); return fail(413, "invalid_request"); } chunks.push(value); }
    const response = await fetch(`${base.replace(/\/$/, "")}/oauth/token${new URL(request.url).search}`, {
      method: "POST", headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new Uint8Array(Buffer.concat(chunks)), cache: "no-store", redirect: "error", signal: AbortSignal.timeout(15000),
    });
    return new Response(response.body, { status: response.status, headers: { ...privateHeaders, Pragma: "no-cache", "Content-Type": "application/json" } });
  } catch { return fail(503, "temporarily_unavailable"); }
}
