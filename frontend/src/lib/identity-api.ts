export type IdentityRole = { id: string; name: string; builtin: boolean; capabilities: string[]; version: number };
export type IdentityCapability = { name: string; human: boolean; service: boolean; client: "" | "normal" | "sensitive"; reserved?: boolean };
export async function identityRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/identity/${path}`, { ...init, cache: "no-store", headers: { "Content-Type": "application/json", ...init?.headers } });
  if (!response.ok) {
    const text = (await response.text()).slice(0, 1000);
    let message = `Identity request failed (${response.status}).`;
    try { const data: unknown = JSON.parse(text); if (data && typeof data === "object" && "error" in data && typeof data.error === "string") message = data.error.slice(0, 300); } catch { if (text && !text.trim().startsWith("<")) message = text.slice(0, 300); }
    throw new Error(message);
  }
  if (response.status === 204 || response.status === 201 && response.headers.get("content-length") === "0") return undefined as T;
  const text = await response.text(); return text ? JSON.parse(text) as T : undefined as T;
}
