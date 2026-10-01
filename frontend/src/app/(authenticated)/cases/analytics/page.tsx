import { CaseAnalyticsPage } from "@/components/cases/analytics";
import { casePageContext } from "@/lib/server/case-page";

export default async function Page({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { tenant, session } = await casePageContext(searchParams);
  const now = new Date();
  const from = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1)).toISOString().slice(0, 10);
  const to = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1)).toISOString().slice(0, 10);
  return <CaseAnalyticsPage key={`${tenant}:${session}`} tenant={tenant} session={session} initialFrom={from} initialTo={to} />;
}
