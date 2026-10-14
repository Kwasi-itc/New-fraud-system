import "server-only";
import { platformSession } from "./auth";

export async function casePageContext(search: Promise<Record<string, string | string[] | undefined>>) {
  const params = await search;
  const current = await platformSession();
  const tenant = typeof params.tenant === "string" ? params.tenant : process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID || current?.session.tenant_id || "";
  const session = current ? `${current.session.id}:${current.session.version}` : "signed-out";
  return { tenant, session, params };
}
