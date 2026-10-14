"use client";

import { useState } from "react";
import {
  AlertCircle,
  CheckCircle2,
  Download,
  FileCheck,
  FileText,
  Plus,
  Save,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import type { CaseFile, CaseReport, ReportContent } from "@/lib/case-manager-api";
import { useCaseLinks, useCaseReports, useCaseWrite } from "@/lib/case-manager-query";
import { Panel, Problem, fieldClass, labelClass, timestamp } from "./shared";
import { StatusPill } from "./structured-data-viewer";
import { cn } from "@/lib/utils";

export function CaseReports({
  tenant,
  session,
  id,
}: {
  tenant: string;
  session: string;
  id: string;
}) {
  const reports = useCaseReports(tenant, session, id);
  const files = useCaseLinks<CaseFile>(tenant, session, id, "files");
  const write = useCaseWrite(tenant, session);
  const [draftID, setDraftID] = useState<string>();
  const evidence = files.error
    ? []
    : files.data?.pages.flatMap((p) => p.items).filter((f) => f.storage_key?.startsWith("case-db/")) ?? [];

  return (
    <Panel title="Suspicious activity reports">
      <p className="text-sm text-slate-500">
        Internal investigation reports. Completion preserves a snapshot; it does not submit a regulatory filing.
      </p>

      <Problem error={reports.error} retry={() => void reports.refetch()} />
      <Problem error={files.error} retry={() => void files.refetch()} />

      {reports.isPending && (
        <p role="status" className="text-sm text-slate-500">
          Loading reports...
        </p>
      )}

      {!reports.error && reports.data && (
        <div className="space-y-4">
          <div className="space-y-3">
            {reports.data.pages
              .flatMap((p) => p.reports)
              .map((report) => (
                <details
                  key={report.id}
                  className="group rounded-xl border border-slate-200 bg-white p-4 transition-all"
                >
                  <summary className="flex cursor-pointer list-none items-center justify-between gap-3 text-sm font-semibold">
                    <div className="flex items-center gap-2">
                      <FileCheck className="size-4 text-blue-600" />
                      <span className="text-slate-900 font-semibold">
                        {report.payload.content?.title || "Legacy report"}
                      </span>
                      <span className="text-slate-400">·</span>
                      <StatusPill value={report.status} />
                      <span className="rounded bg-slate-100 px-1.5 py-0.5 text-sm font-mono text-slate-600">
                        v{report.version}
                      </span>
                    </div>
                  </summary>

                  <div className="mt-4 pt-3 border-t border-slate-100 space-y-4">
                    <p className="text-sm text-slate-500">
                      Created by {report.created_by}
                      {report.completed_at &&
                        ` · Completed by ${report.completed_by ?? "unknown"} at ${timestamp(
                          report.completed_at
                        )}`}
                    </p>

                    {report.payload.format === "internal_sar_v1" ? (
                      <ReportEditor
                        key={report.version}
                        report={report}
                        files={evidence}
                        tenant={tenant}
                        session={session}
                        id={id}
                      />
                    ) : (
                      <p className="text-sm text-slate-500 italic">
                        Legacy format. Download the original payload to inspect this report.
                      </p>
                    )}

                    <div className="pt-2 border-t border-slate-100">
                      <a
                        className="inline-flex items-center gap-1.5 text-sm font-medium text-blue-700 hover:underline"
                        href={`/api/cases/tenants/${tenant}/cases/${id}/reports/${report.id}/export`}
                      >
                        <Download className="size-3.5" />
                        Download report JSON
                      </a>
                    </div>
                  </div>
                </details>
              ))}
          </div>

          {!reports.data.pages.some((p) => p.reports.length) && (
            <p className="text-sm text-slate-500 italic py-2">No reports yet.</p>
          )}

          <div className="flex flex-wrap gap-2">
            {reports.hasNextPage && (
              <Button
                variant="outline"
                size="sm"
                disabled={reports.isFetchingNextPage}
                onClick={() => void reports.fetchNextPage()}
                className="text-sm"
              >
                More reports
              </Button>
            )}
            {files.hasNextPage && (
              <Button
                variant="outline"
                size="sm"
                disabled={files.isFetchingNextPage}
                onClick={() => void files.fetchNextPage()}
                className="text-sm"
              >
                Load more report attachment choices
              </Button>
            )}
          </div>

          <Problem error={write.error} />

          {!draftID ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setDraftID(crypto.randomUUID())}
              className="gap-1.5 text-sm font-medium"
            >
              <Plus className="size-3.5" />
              New report
            </Button>
          ) : (
            <form
              className="space-y-3 rounded-xl border border-slate-200 bg-slate-50/70 p-4"
              onSubmit={async (event) => {
                event.preventDefault();
                const data = new FormData(event.currentTarget);
                try {
                  await write.mutateAsync({
                    path: `cases/${id}/reports`,
                    payload: {
                      id: draftID,
                      content: {
                        title: data.get("title"),
                        subject: "",
                        narrative: "",
                        file_ids: [],
                      },
                    },
                  });
                  setDraftID(undefined);
                } catch {
                  /* Mutation error remains visible; keep the retry ID and input. */
                }
              }}
            >
              <label className={labelClass}>
                <span>New report title</span>
                <input
                  name="title"
                  required
                  maxLength={200}
                  className={cn(fieldClass, "font-normal normal-case")}
                  placeholder="e.g. SAR Filing Draft - Unexplained High-Volume Transfers"
                />
              </label>
              <div className="flex gap-2">
                <Button size="sm" disabled={write.isPending} className="text-sm">
                  Create draft
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  type="button"
                  onClick={() => setDraftID(undefined)}
                  className="text-sm"
                >
                  Cancel
                </Button>
              </div>
            </form>
          )}
        </div>
      )}
    </Panel>
  );
}

function ReportEditor({
  report,
  files,
  tenant,
  session,
  id,
}: {
  report: CaseReport;
  files: CaseFile[];
  tenant: string;
  session: string;
  id: string;
}) {
  const write = useCaseWrite(tenant, session);
  const content = report.payload.content;
  const [selected, setSelected] = useState<string[]>(content.file_ids ?? []);
  const [dirty, setDirty] = useState(false);
  const completed = report.status === "completed";

  return (
    <form
      className="space-y-4"
      onChange={() => setDirty(true)}
      onSubmit={async (event) => {
        event.preventDefault();
        const data = new FormData(event.currentTarget);
        const from = String(data.get("from"));
        const to = String(data.get("to"));
        const next: ReportContent = {
          title: String(data.get("title")),
          subject: String(data.get("subject")),
          narrative: String(data.get("narrative")),
          file_ids: selected,
          activity_from: from ? new Date(from + "Z").toISOString() : undefined,
          activity_to: to ? new Date(to + "Z").toISOString() : undefined,
        };
        try {
          await write.mutateAsync({
            path: `cases/${id}/reports/${report.id}`,
            method: "PATCH",
            payload: { version: report.version, content: next },
          });
        } catch {
          /* Preserve edited content and show the conflict/failure. */
        }
      }}
    >
      <Problem error={write.error} />
      <fieldset disabled={completed || write.isPending} className="space-y-4">
        <label className={labelClass}>
          <span>Report title</span>
          <input
            name="title"
            defaultValue={content.title}
            required
            maxLength={200}
            className={cn(fieldClass, "font-normal normal-case")}
          />
        </label>

        <label className={labelClass}>
          <span>Report subject</span>
          <textarea
            name="subject"
            defaultValue={content.subject}
            maxLength={1000}
            rows={2}
            className={cn(fieldClass, "font-normal normal-case")}
            placeholder="Subject identity, aliases, known entity metadata..."
          />
        </label>

        <label className={labelClass}>
          <span>Report narrative</span>
          <textarea
            name="narrative"
            defaultValue={content.narrative}
            maxLength={50000}
            rows={5}
            className={cn(fieldClass, "font-normal normal-case")}
            placeholder="Chronological explanation of suspicious activity, flags, transaction patterns, and conclusion..."
          />
        </label>

        <div className="grid gap-3 sm:grid-cols-2">
          <label className={labelClass}>
            <span>Activity start (UTC)</span>
            <input
              type="datetime-local"
              step="1"
              name="from"
              defaultValue={
                content.activity_from
                  ? new Date(content.activity_from).toISOString().slice(0, 19)
                  : undefined
              }
              className={cn(fieldClass, "font-normal normal-case")}
            />
          </label>
          <label className={labelClass}>
            <span>Activity end (UTC)</span>
            <input
              type="datetime-local"
              step="1"
              name="to"
              defaultValue={
                content.activity_to
                  ? new Date(content.activity_to).toISOString().slice(0, 19)
                  : undefined
              }
              className={cn(fieldClass, "font-normal normal-case")}
            />
          </label>
        </div>

        <fieldset className="space-y-2 rounded-lg border border-slate-200 bg-slate-50/50 p-3">
          <legend className="text-sm font-semibold uppercase tracking-wider text-slate-600">
            Attached case evidence
          </legend>
          {Array.from(new Set([...files.map((f) => f.id), ...selected])).map((fileID) => (
            <label key={fileID} className="flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
              <input
                type="checkbox"
                checked={selected.includes(fileID)}
                onChange={(event) =>
                  setSelected((previous) =>
                    event.target.checked
                      ? [...previous, fileID]
                      : previous.filter((v) => v !== fileID)
                  )
                }
                className="rounded border-slate-300 text-blue-600 focus:ring-blue-500"
              />
              <span className="truncate">{files.find((f) => f.id === fileID)?.file_name ?? fileID}</span>
            </label>
          ))}
          {!files.length && !selected.length && (
            <p className="text-sm text-slate-500 italic">Upload case evidence to attach it here.</p>
          )}
        </fieldset>

        {!completed && (
          <Button type="submit" size="sm" variant="outline" className="text-sm gap-1.5">
            <Save className="size-3.5" />
            Save report draft
          </Button>
        )}
      </fieldset>

      {!completed && (
        <div className="pt-2 border-t border-slate-100 flex flex-wrap items-center justify-between gap-3">
          <p className="text-sm text-slate-500">
            Save changes before completing. Completed reports cannot be edited.
          </p>
          <Button
            type="button"
            size="sm"
            disabled={dirty || write.isPending}
            onClick={() =>
              void write
                .mutateAsync({
                  path: `cases/${id}/reports/${report.id}/complete`,
                  payload: { version: report.version },
                })
                .catch(() => {})
            }
            className="text-sm font-medium"
          >
            Complete report
          </Button>
        </div>
      )}
    </form>
  );
}
