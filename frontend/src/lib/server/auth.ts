import "server-only";
import { bffAuthToken } from "./service-token";
import { cookies } from "next/headers";
export type PlatformSession = { id: string; user_id: string; tenant_id: string; name: string; roles: string[]; capabilities: string[]; version: number; expires_at: string };
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
  const base = process.env.AUTH_SERVICE_URL, token = bffAuthToken();
  if (!base || !token) throw new Error("Authentication is not configured.");
  return fetch(`${base.replace(/\/$/, "")}/internal/${path}`, { method: "POST", headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" }, body: JSON.stringify(body), cache: "no-store", redirect: "error", signal: AbortSignal.timeout(15000) });
}
export async function humanAuthCall(accessToken: string, path: string, init: RequestInit = {}) {
  const base = process.env.AUTH_SERVICE_URL;
  if (!base || !accessToken) throw new Error("Authentication is not configured.");
  const headers = new Headers(init.headers);
  headers.set("Authorization", `Bearer ${accessToken}`);
  if (init.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  return fetch(`${base.replace(/\/$/, "")}/v1/${path.replace(/^\/+/, "")}`, {
    ...init, headers, cache: "no-store", redirect: "error", signal: AbortSignal.timeout(15000),
  });
}
export async function rawSession() { return (await cookies()).get(sessionCookie())?.value; }
export async function platformSession(): Promise<{ session: PlatformSession; access_token: string } | null> {
  const raw = await rawSession(); if (!raw) return null;
  const response = await authCall("session", { session: raw });
  if (response.status === 401 || response.status === 403) return null;
  if (!response.ok) throw new Error("Authentication is temporarily unavailable.");
  return response.json();
}

export type AccountProfile = { user_id: string; tenant_id: string; tenant_name: string; name: string; account_name: string; email: string; email_verified: boolean; role_id: string; role_name: string; version: number };
export async function accountProfile(): Promise<AccountProfile | null> {
  const current = await platformSession();
  if (!current) return null;
  const response = await humanAuthCall(current.access_token, "account/profile");
  if (response.status === 401 || response.status === 403) return null;
  if (!response.ok) throw new Error("Your account details are temporarily unavailable.");
  return response.json();
}

export function manageSignInURL(): string | null {
  const configured = process.env.AUTH_MANAGE_SIGN_IN_URL;
  if (!configured) return null;
  const url = new URL(configured);
  if (url.username || url.password || url.search || url.hash || (url.protocol !== "https:" && !(url.protocol === "http:" && ["localhost", "127.0.0.1"].includes(url.hostname)))) {
    throw new Error("AUTH_MANAGE_SIGN_IN_URL must be a trusted HTTPS account page (local HTTP is permitted).");
  }
  return url.href;
}
