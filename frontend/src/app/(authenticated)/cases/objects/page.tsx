import { CaseObject } from "@/components/cases/case-object";
import { casePageContext } from "@/lib/server/case-page";
export default async function Page({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { tenant, session, params } = await casePageContext(searchParams);
  const value = (key: string) => typeof params[key] === "string" ? params[key] as string : "";
  return <CaseObject key={`${tenant}:${session}:${value("caseId")}:${value("objectId")}`} tenant={tenant} session={session} caseId={value("caseId")} objectType={value("objectType")} objectId={value("objectId")} />;
}
