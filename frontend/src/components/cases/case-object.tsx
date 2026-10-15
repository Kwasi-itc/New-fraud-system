"use client";

import Link from "next/link";
import { ArrowLeft, Database, Shield, ExternalLink } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  useCaseLinks,
  useCaseQuery,
  useCaseQueue,
} from "@/lib/case-manager-query";
import type { CaseRecord, DecisionLink } from "@/lib/case-manager-api";
import { CaseNav, Panel, Problem, readable } from "./shared";
import { Evidence } from "./evidence";
import { CopyButton, StatusPill } from "./structured-data-viewer";

import { useRecordDisplays, displayValue } from "@/components/detection/decision-display";

export function CaseObject({
  tenant,
  session,
  caseId,
  objectType,
  objectId,
}: {
  tenant: string;
  session: string;
  caseId: string;
  objectType: string;
  objectId: string;
}) {
  const detail = useCaseQuery<{ case: CaseRecord }>(
    tenant,
    session,
    `cases/${caseId}/overview`
  );
  const links = useCaseLinks<DecisionLink>(tenant, session, caseId, "decisions");
  const related = useCaseQueue(
    tenant,
    session,
    `related_to=${caseId}&include_snoozed=true`,
    detail.isSuccess
  );

  const objectRefs = [{ object_type: objectType, object_id: objectId }];
  const displays = useRecordDisplays(tenant, session, objectRefs, objectType);
  const currentRecord = displays.record({ object_type: objectType, object_id: objectId });

  if (detail.error)
    return <Problem error={detail.error} retry={() => void detail.refetch()} />;
  if (!detail.data)
    return (
      <div className="flex items-center gap-2 py-12 text-slate-500 text-sm">
        <span className="inline-block size-3.5 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
        <p>Loading object investigation…</p>
      </div>
    );

  const matching =
    links.data?.pages
      .flatMap((page) => page.items)
      .filter(
        (link) => link.object_type === objectType && link.object_id === objectId
      ) ?? [];

  return (
    <div className="space-y-6">
      <CaseNav tenant={tenant} />

      {/* Entity Profile Banner */}
      <header className="rounded-2xl border border-slate-200 bg-white p-6 shadow-xs">
        <Link
          className="inline-flex items-center gap-1.5 text-sm font-semibold text-blue-700 hover:underline mb-3"
          href={`/cases/${caseId}?tenant=${tenant}`}
        >
          <ArrowLeft className="size-3.5" />
          Back to case: {detail.data.case.name}
        </Link>

        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-slate-900">
              {currentRecord?.caption && currentRecord.caption !== objectId
                ? currentRecord.caption
                : "Object Investigation"}
            </h1>
            <div className="mt-2.5 flex flex-wrap items-center gap-2 text-sm">
              <span className="rounded-md bg-blue-50 px-2.5 py-0.5 font-bold text-blue-700 uppercase tracking-wider text-xs border border-blue-200">
                {objectType}
              </span>
              <span className="font-mono text-slate-800 font-semibold bg-slate-100 px-2.5 py-0.5 rounded-md border border-slate-200">
                {objectId}
              </span>
              <CopyButton text={objectId} />
              {currentRecord?.found === false && (
                <span className="text-xs text-amber-700 bg-amber-50 border border-amber-200 rounded px-2 py-0.5">
                  Record unavailable in source table
                </span>
              )}
            </div>
          </div>
        </div>

        {/* Resolved Display Attributes */}
        {displays.columns.length > 0 && currentRecord?.values && (
          <div className="mt-4 pt-4 border-t border-slate-100 flex flex-wrap gap-2.5">
            {displays.columns.map((col) => (
              <div
                key={col.id}
                className="rounded-lg bg-slate-50 border border-slate-200 px-3 py-1.5 text-xs flex items-center gap-1.5"
              >
                <span className="text-slate-500 font-medium">{col.label}:</span>
                <span className="text-slate-900 font-semibold">
                  {displayValue(currentRecord.values[col.id])}
                </span>
              </div>
            ))}
          </div>
        )}
      </header>

      {/* Decision Evidence */}
      <Panel title="Decision evidence for this object">
        <Problem error={links.error} retry={() => void links.refetch()} />
        {!links.error &&
          matching.map((link) => (
            <div
              key={link.id}
              className="space-y-3 rounded-xl border border-slate-200 bg-slate-50/40 p-4"
            >
              <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200/60 pb-2">
                <div className="flex items-center gap-2">
                  <Shield className="size-4 text-blue-600" />
                  <span className="font-mono text-sm font-bold text-slate-800">
                    Decision {link.decision_id}
                  </span>
                  <CopyButton text={link.decision_id} />
                </div>
              </div>
              <Evidence
                tenant={tenant}
                session={session}
                caseId={caseId}
                kind="decisions"
                resource={link.decision_id}
              />
            </div>
          ))}

        {links.hasNextPage && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => void links.fetchNextPage()}
            className="text-sm"
          >
            Search more linked decisions
          </Button>
        )}
        {links.isSuccess && !links.hasNextPage && matching.length === 0 && (
          <p className="text-sm text-slate-500 italic py-2">
            No linked decision references this object.
          </p>
        )}
      </Panel>

      {/* Related Cases */}
      <Panel title="Cases related to this investigation">
        <Problem error={related.error} retry={() => void related.refetch()} />
        <ul className="divide-y divide-slate-100">
          {!related.error &&
            related.data?.pages
              .flatMap((page) => page.cases)
              .map((item) => (
                <li
                  className="py-3 flex items-center justify-between gap-3 text-sm"
                  key={item.id}
                >
                  <Link
                    className="font-medium text-blue-700 hover:underline"
                    href={`/cases/${item.id}?tenant=${tenant}`}
                  >
                    {item.name}
                  </Link>
                  <StatusPill value={item.status} />
                </li>
              ))}
        </ul>
        {related.hasNextPage && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => void related.fetchNextPage()}
            className="text-sm mt-2"
          >
            More related cases
          </Button>
        )}
      </Panel>
    </div>
  );
}
