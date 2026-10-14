import { CaseSettings } from "@/components/cases/case-settings";
import { casePageContext } from "@/lib/server/case-page";

export default async function Page({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { tenant, session } = await casePageContext(searchParams);
  return <CaseSettings key={`${tenant}:${session}:tags`} tenant={tenant} session={session} defaultTab="tags" />;
}
