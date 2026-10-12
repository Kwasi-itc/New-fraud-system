import "server-only";
import { cookies } from "next/headers";
export type PlatformSession = { id: string; user_id: string; tenant_id: string; name: string; roles: string[]; version: number; expires_at: string };
export const sessionCookie = () => process.env.CASE_SESSION_COOKIE ?? "__Host-fraud_session";
export const loginCookie = "__Host-fraud_login";
export const cookieOptions = { httpOnly: true, secure: true, sameSite: "lax" as const, path: "/" };
export const privateHeaders = { "Cache-Control": "private, no-store" };
export async function authCall(path: string, body: unknown) {
  const base = process.env.AUTH_SERVICE_URL, token = process.env.AUTH_PROXY_TOKEN;
  if (!base || !token) throw new Error("Authentication is not configured.");
  return fetch(`${base.replace(/\/$/, "")}/internal/${path}`, { method: "POST", headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" }, body: JSON.stringify(body), cache: "no-store", redirect: "error", signal: AbortSignal.timeout(15000) });
}
export async function rawSession() { return (await cookies()).get(sessionCookie())?.value; }
export async function platformSession(): Promise<{ session: PlatformSession; access_token: string } | null> {
  const raw = await rawSession(); if (!raw) return null;
  const response = await authCall("session", { session: raw });
  if (response.status === 401 || response.status === 403) return null;
  if (!response.ok) throw new Error("Authentication is temporarily unavailable.");
  return response.json();
}
