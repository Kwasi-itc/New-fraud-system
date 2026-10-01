import { CaseQueue } from "@/components/cases/case-queue";
import { casePageContext } from "@/lib/server/case-page";

export default async function CasesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { tenant, session } = await casePageContext(searchParams);
  return <CaseQueue key={`${tenant}:${session}`} tenant={tenant} session={session} />;
}
