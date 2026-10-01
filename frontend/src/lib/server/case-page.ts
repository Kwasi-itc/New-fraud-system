import "server-only";
import { createHash } from "node:crypto";
import { caseToken } from "./case-proxy";

export async function casePageContext(search: Promise<Record<string, string | string[] | undefined>>) {
  const params = await search;
  const tenant = typeof params.tenant === "string" ? params.tenant : process.env.CASE_DEFAULT_TENANT_ID ?? process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const token = await caseToken();
  const session = token ? createHash("sha256").update(token).digest("hex") : "signed-out";
  return { tenant, session, params };
}
