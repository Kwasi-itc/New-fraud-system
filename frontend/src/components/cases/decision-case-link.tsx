"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { writeCase, type DecisionCaseMembership } from "@/lib/case-manager-api";
import { caseKeys } from "@/lib/case-manager-query";

export function DecisionCaseLink({ tenant, session, decisionId }: {
  tenant: string;
  session: string;
  decisionId: string;
}) {
  const membership = useQuery({
    queryKey: [...caseKeys.scope(tenant, session), "decision-memberships", [decisionId]],
    queryFn: () => writeCase<{ decisions: DecisionCaseMembership[] }>(
      tenant, "decision-case-memberships", { decision_ids: [decisionId] }
    ),
    enabled: Boolean(tenant && decisionId),
    retry: false,
    staleTime: 0,
    gcTime: 0,
  });

  if (membership.isError) {
    return <span role="alert">Could not load case. <button type="button" className="text-blue-700 underline" onClick={() => void membership.refetch()}>Retry case lookup</button></span>;
  }
  if (membership.isPending) return <span role="status">Loading case...</span>;

  const entry = membership.data.decisions.find(item => item.decision_id === decisionId);
  if (entry?.state === "unlinked") return <span>No case</span>;
  if (entry?.state !== "linked" || !entry.case_id || !entry.case_name) {
    return <span>Case unavailable</span>;
  }
  return (
    <Link className="font-medium text-blue-700 underline hover:text-blue-900"
      href={`/cases/${encodeURIComponent(entry.case_id)}?${new URLSearchParams({ tenant })}`}>
      {entry.case_name}
    </Link>
  );
}
