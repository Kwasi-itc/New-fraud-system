import { InboxSettings } from "@/components/cases/inbox-settings";
import { casePageContext } from "@/lib/server/case-page";
export default async function Page({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { tenant, session } = await casePageContext(searchParams);
  return <InboxSettings key={`${tenant}:${session}`} tenant={tenant} session={session} />;
}
