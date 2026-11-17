import "server-only";
import { cookies } from "next/headers";
export type PlatformSession = { id: string; user_id: string; tenant_id: string; name: string; roles: string[]; version: number; expires_at: string };
const secureCookieSetting = process.env.AUTH_COOKIE_SECURE ?? "true";
if (secureCookieSetting !== "true" && secureCookieSetting !== "false") {
  throw new Error("AUTH_COOKIE_SECURE must be true or false.");
}
const secureCookies = secureCookieSetting === "true";
const configuredSessionCookie = process.env.CASE_SESSION_COOKIE;
if (!secureCookies && (configuredSessionCookie?.startsWith("__Host-") || configuredSessionCookie?.startsWith("__Secure-"))) {
  throw new Error("CASE_SESSION_COOKIE cannot use a __Host- or __Secure- prefix when AUTH_COOKIE_SECURE=false.");
}
export const sessionCookie = () => configuredSessionCookie ?? (secureCookies ? "__Host-fraud_session" : "fraud_session");
export const loginCookie = secureCookies ? "__Host-fraud_login" : "fraud_login";
export const cookieOptions = { httpOnly: true, secure: secureCookies, sameSite: "lax" as const, path: "/" };
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
