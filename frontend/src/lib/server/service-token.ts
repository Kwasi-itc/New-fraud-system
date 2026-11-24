import "server-only";
import { createPrivateKey, createSign, randomUUID, type KeyObject } from "node:crypto";
import { readFileSync } from "node:fs";

let signingKey: KeyObject | undefined;
export function bffAuthToken(): string {
  const file = process.env.PLATFORM_AUTH_SIGNING_KEY_FILE;
  const kid = process.env.PLATFORM_AUTH_SIGNING_KEY_ID;
  const service = process.env.PLATFORM_AUTH_SERVICE_ID;
  if (!file || !kid || service !== "frontend-bff") throw new Error("BFF signing identity is not configured.");
  signingKey ??= createPrivateKey(readFileSync(file));
  if (signingKey.asymmetricKeyType !== "rsa" || (signingKey.asymmetricKeyDetails?.modulusLength ?? 0) < 2048) throw new Error("BFF requires an RSA signing key of at least 2048 bits.");
  const now = Math.floor(Date.now() / 1000);
  const header = Buffer.from(JSON.stringify({ alg: "RS256", typ: "at+jwt", kid })).toString("base64url");
  const payload = Buffer.from(JSON.stringify({
    iss: `urn:fraud:service:${service}`, sub: service, service_id: service,
    principal_type: "service", aud: "urn:fraud:auth-service",
    tenant_id: "00000000-0000-0000-0000-000000000000",
    capabilities: ["auth:bootstrap"], orig_type: "service", orig_id: service,
    iat: now, exp: now + 60, jti: randomUUID(),
  })).toString("base64url");
  const input = `${header}.${payload}`;
  return `${input}.${createSign("RSA-SHA256").update(input).end().sign(signingKey, "base64url")}`;
}

