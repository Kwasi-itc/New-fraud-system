import type { PlatformSession } from "./auth";

// Only the dedicated local MySQL Compose project sets this server-side switch.
// This is a development identity, never a credential issued by the auth owner.
export function localMySQLSession(env: NodeJS.ProcessEnv = process.env): { session: PlatformSession; access_token: string } | null {
  if (!env.LOCAL_MYSQL_NO_AUTH) return null;
  if (env.LOCAL_MYSQL_NO_AUTH !== "1") throw new Error("Invalid local MySQL mode.");
  const origin = new URL(env.APP_ORIGIN ?? "");
  if (origin.protocol !== "http:" || !["localhost", "127.0.0.1", "[::1]"].includes(origin.hostname) || origin.username || origin.password || origin.search || origin.hash || origin.pathname !== "/") {
    throw new Error("Local MySQL mode requires a loopback HTTP application origin.");
  }
  const tenant = env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID;
  if (!tenant || !/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(tenant)) {
    throw new Error("Local MySQL mode requires an explicit tenant UUID.");
  }
  return {
    session: {
      id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
      user_id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
      tenant_id: tenant,
      name: "Local developer (authentication disabled)",
      roles: ["platform_viewer", "platform_builder", "platform_publisher", "platform_admin"],
      version: 1,
      expires_at: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
    },
    access_token: "local-mysql-development-only",
  };
}
