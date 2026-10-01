"use client";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { useCaseLinks, useCaseQuery, useCaseQueue } from "@/lib/case-manager-query";
import type { CaseRecord, DecisionLink } from "@/lib/case-manager-api";
import { CaseNav, Panel, Problem, readable } from "./shared";
import { Evidence } from "./evidence";

export function CaseObject({ tenant, session, caseId, objectType, objectId }: { tenant: string; session: string; caseId: string; objectType: string; objectId: string }) {
  const detail = useCaseQuery<{ case: CaseRecord }>(tenant, session, `cases/${caseId}/overview`);
  const links = useCaseLinks<DecisionLink>(tenant, session, caseId, "decisions");
  const related = useCaseQueue(tenant, session, `related_to=${caseId}&include_snoozed=true`, detail.isSuccess);
  if (detail.error) return <Problem error={detail.error} retry={() => void detail.refetch()} />;
  if (!detail.data) return <p>Loading object investigation…</p>;
  const matching = links.data?.pages.flatMap(page => page.items).filter(link => link.object_type === objectType && link.object_id === objectId) ?? [];
  return <div className="space-y-6"><CaseNav tenant={tenant} /><h1 className="text-3xl font-semibold">Object investigation</h1><p>{objectType}: {objectId}</p><Link className="text-blue-700" href={`/cases/${caseId}?tenant=${tenant}`}>Back to {detail.data.case.name}</Link>
    <Panel title="Decision evidence for this object"><Problem error={links.error} retry={() => void links.refetch()} />{!links.error && matching.map(link => <div key={link.id} className="space-y-3 border-b pb-4"><p>{link.decision_id}</p><Evidence tenant={tenant} session={session} caseId={caseId} kind="decisions" resource={link.decision_id} /></div>)}{links.hasNextPage && <Button variant="outline" onClick={() => void links.fetchNextPage()}>Search more linked decisions</Button>}{links.isSuccess && !links.hasNextPage && matching.length === 0 && <p>No linked decision references this object.</p>}</Panel>
    <Panel title="Cases related to this investigation"><Problem error={related.error} retry={() => void related.refetch()} /><ul>{!related.error && related.data?.pages.flatMap(page => page.cases).map(item => <li className="py-2" key={item.id}><Link className="text-blue-700" href={`/cases/${item.id}?tenant=${tenant}`}>{item.name}</Link> · {readable(item.status)}</li>)}</ul>{related.hasNextPage && <Button variant="outline" onClick={() => void related.fetchNextPage()}>More related cases</Button>}</Panel>
  </div>;
}
