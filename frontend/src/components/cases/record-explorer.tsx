"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  Bookmark,
  ChevronRight,
  Database,
  GitBranch,
  Network,
  Share2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { requestCase, writeCase } from "@/lib/case-manager-api";
import { caseKeys } from "@/lib/case-manager-query";
import { displayValue, useRecordDisplays } from "@/components/detection/decision-display";
import { Problem, readable } from "./shared";
import { CopyButton, StatusPill, ValueDisplay } from "./structured-data-viewer";

type Hop = { connection: string; object_id: string; label: string; relationshipLabel?: string };
type Connection = { id: string; label: string; target: string; reason: string };
type AssociatedDecision = {
  id: string;
  scenario_name: string;
  outcome: string;
  score: number;
  triggered: boolean;
  created_at: string;
  case?: { id: string; name: string; status: string };
  can_attach: boolean;
};
type Result = {
  table: string;
  current: { object_id: string; data: Record<string, unknown> };
  connections: Connection[];
  records: { object_id: string }[];
  next_after: string;
  decisions: AssociatedDecision[];
  next_decision_after: string;
};

export function RecordExplorer({
  tenant,
  session,
  caseId,
  decisionId,
}: {
  tenant: string;
  session: string;
  caseId: string;
  decisionId: string;
}) {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [path, setPath] = useState<Hop[]>([]);
  const [connection, setConnection] = useState("");
  const [after, setAfter] = useState("");
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState("");
  const [saveError, setSaveError] = useState<Error | null>(null);
  const [decisionAfter, setDecisionAfter] = useState("");
  const [attaching, setAttaching] = useState("");

  const query = useQuery<Result>({
    queryKey: [
      ...caseKeys.scope(tenant, session),
      caseId,
      "explore",
      decisionId,
      path,
      connection,
      after,
      decisionAfter,
    ],
    enabled: open,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    queryFn: ({ signal }) =>
      requestCase(tenant, `cases/${caseId}/explore`, {
        method: "POST",
        signal,
        body: JSON.stringify({
          decision_id: decisionId,
          path: path.map(({ connection, object_id }) => ({ connection, object_id })),
          connection,
          after,
          decision_after: decisionAfter,
        }),
      }),
  });

  function navigate(next: Hop[]) {
    setPath(next);
    setConnection("");
    setAfter("");
    setDecisionAfter("");
    setNotice("");
    setSaveError(null);
  }

  const data = query.error ? undefined : query.data;
  const selected = data?.connections.find((e) => e.id === connection);

  const objectsToDisplay = useMemo(() => {
    const list: Array<{ object_type: string; object_id: string }> = [];
    if (data?.table && data?.current?.object_id) {
      list.push({ object_type: data.table, object_id: data.current.object_id });
    }
    if (selected?.target && data?.records) {
      for (const r of data.records) {
        list.push({ object_type: selected.target, object_id: r.object_id });
      }
    }
    for (const h of path) {
      if (h.label) {
        list.push({ object_type: h.label, object_id: h.object_id });
      }
    }
    return list;
  }, [data?.table, data?.current?.object_id, data?.records, selected?.target, path]);

  const displays = useRecordDisplays(
    tenant,
    session,
    objectsToDisplay,
    undefined,
    open && objectsToDisplay.length > 0
  );
  const currentRecordDisplay =
    data && data.table && data.current
      ? displays.record({ object_type: data.table, object_id: data.current.object_id })
      : undefined;

  async function attachDecision(target: AssociatedDecision) {
    if (
      !window.confirm(
        `Attach decision ${target.id} to this case? This may start the investigation and assign you under the inbox rules. Decisions in another case require an explicit transfer.`
      )
    )
      return;
    setAttaching(target.id);
    setSaveError(null);
    setNotice("");
    try {
      await writeCase(tenant, `cases/${caseId}/explore/attach`, {
        decision_id: decisionId,
        path: path.map(({ connection, object_id }) => ({ connection, object_id })),
        target_decision_id: target.id,
      });
      setNotice("Decision attached to this case.");
    } catch (e) {
      setSaveError(
        e instanceof Error
          ? e
          : new Error("Attachment could not be confirmed. Refresh before retrying.")
      );
    } finally {
      setAttaching("");
      void queryClient.invalidateQueries({ queryKey: caseKeys.scope(tenant, session) });
    }
  }

  async function saveTrail() {
    if (
      !data ||
      !window.confirm(
        "Add this investigation path to the case notes? This records the path, not a snapshot of the live records."
      )
    )
      return;
    setSaving(true);
    setSaveError(null);
    try {
      // Generate clean display fields for the terminal record
      const tableCols = displays.allColumns[data.table] ?? [];
      const terminalFields: string[] = [];
      if (tableCols.length > 0 && currentRecordDisplay?.values) {
        for (const col of tableCols) {
          const val = displayValue(currentRecordDisplay.values[col.id]);
          if (val && val !== "—") {
            terminalFields.push(`• **${col.label}**: ${val}`);
          }
        }
      }

      // Format terminal record caption / title
      const terminalCaption =
        currentRecordDisplay?.caption &&
        currentRecordDisplay.caption !== data.current.object_id &&
        !currentRecordDisplay.caption.startsWith("production-replay:")
          ? currentRecordDisplay.caption
          : "";
      const terminalTitle = terminalCaption
        ? `${readable(data.table)} · ${terminalCaption}`
        : readable(data.table);

      // Generate human-friendly exploration steps
      const trailSteps = path
        .map((h, i) => {
          const hopDisplay = displays.record({ object_type: h.label, object_id: h.object_id });
          const hopCaption =
            hopDisplay?.caption &&
            hopDisplay.caption !== h.object_id &&
            !hopDisplay.caption.startsWith("production-replay:")
              ? hopDisplay.caption
              : "";

          const hopCols = (displays.allColumns[h.label] ?? []).slice(0, 3);
          const hopFieldValues: string[] = [];
          if (hopCols.length > 0 && hopDisplay?.values) {
            for (const col of hopCols) {
              const v = displayValue(hopDisplay.values[col.id]);
              if (v && v !== "—") {
                hopFieldValues.push(`${col.label}: ${v}`);
              }
            }
          }

          let relLabel = h.relationshipLabel || h.connection;
          if (relLabel.startsWith("link:") || relLabel.startsWith("reverse:")) {
            relLabel = "Related connection";
          }
          let relEnglish = readable(relLabel);
          if (relEnglish.toLowerCase().endsWith("(reverse)")) {
            const base = relEnglish.replace(/\(reverse\)/i, "").trim();
            relEnglish = `Reverse ${readable(base)}`;
          } else if (relEnglish.toLowerCase() === "shared pivot") {
            relEnglish = "Shared pivot link";
          }
          const entityLabel = readable(h.label);
          const namePart = hopCaption ? `${entityLabel} · ${hopCaption}` : entityLabel;
          const extraPart = hopFieldValues.length > 0 ? ` (${hopFieldValues.join(", ")})` : "";
          return `  ${i + 1}. Connected via ${relEnglish} → ${namePart}${extraPart}`;
        })
        .join("\n");

      const commentLines = [
        "### Investigation Connection Trail",
        `• **Terminal Record**: ${terminalTitle}`,
        `• **Observed**: ${new Date().toISOString()}`,
      ];

      if (terminalFields.length > 0) {
        commentLines.push("", "**Display Fields:**", ...terminalFields);
      }

      commentLines.push(
        "",
        "**Exploration Path:**",
        trailSteps || "  (Root entity direct inspection)",
        "",
        "_Note: Live graph traversal record; not a static evidence snapshot._"
      );
      const commentBody = commentLines.join("\n");

      await writeCase(tenant, `cases/${caseId}/comments`, {
        comment: commentBody,
      });
      setNotice("Connection trail added to case notes.");
    } catch (e) {
      setSaveError(e instanceof Error ? e : new Error("Could not save the connection trail."));
    } finally {
      setSaving(false);
      void queryClient.invalidateQueries({ queryKey: caseKeys.scope(tenant, session) });
    }
  }

  return (
    <div className="space-y-3">
      <Button
        size="sm"
        variant="outline"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="gap-1.5 font-medium border-slate-200 hover:bg-slate-50 transition-colors"
      >
        <Network className="size-3.5 text-blue-600" />
        {open ? "Hide connections" : "Explore connections"}
      </Button>

      {open && (
        <section
          aria-label="Record connections"
          className="space-y-4 rounded-xl border border-slate-200 bg-white p-5 shadow-xs transition-all"
        >
          <div className="border-b border-slate-100 pb-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h3 className="text-sm font-bold text-slate-900 flex items-center gap-2">
                <GitBranch className="size-4 text-blue-600" />
                Entity Relationship Explorer
              </h3>
              <span className="text-xs font-semibold px-2 py-0.5 rounded-full bg-slate-100 text-slate-600 border border-slate-200">
                {path.length}/6 hops
              </span>
            </div>

          </div>

          {/* Navigation Trail */}
          <nav
            aria-label="Investigation trail"
            className="flex flex-wrap items-center gap-1.5 rounded-lg bg-slate-50 border border-slate-200/80 p-2 text-xs"
          >
            <button
              type="button"
              className={
                path.length === 0
                  ? "rounded px-2.5 py-1 font-bold text-blue-700 bg-white shadow-2xs border border-blue-200 cursor-pointer"
                  : "rounded px-2.5 py-1 text-slate-600 hover:bg-white hover:text-slate-900 transition-colors cursor-pointer"
              }
              onClick={() => navigate([])}
            >
              Root Decision Record
            </button>
            {path.map((hop, index) => {
              const hopDisplay = hop.label
                ? displays.record({ object_type: hop.label, object_id: hop.object_id })
                : undefined;
              const rawCaption = hopDisplay?.caption;
              const caption =
                rawCaption && !rawCaption.startsWith("production-replay:") && rawCaption !== hop.object_id
                  ? rawCaption
                  : null;
              const isLast = index === path.length - 1;
              return (
                <span key={index} className="flex items-center gap-1.5">
                  <ChevronRight className="size-3 text-slate-400" />
                  <button
                    type="button"
                    className={
                      isLast
                        ? "rounded px-2.5 py-1 font-bold text-blue-700 bg-white shadow-2xs border border-blue-200 cursor-pointer"
                        : "rounded px-2.5 py-1 text-slate-600 hover:bg-white hover:text-slate-900 transition-colors cursor-pointer"
                    }
                    onClick={() => navigate(path.slice(0, index + 1))}
                  >
                    <span className="capitalize">{hop.label}</span>
                    {caption ? `: ${caption}` : ""}
                  </button>
                </span>
              );
            })}
          </nav>

          <Problem error={query.error} retry={() => void query.refetch()} />
          {query.isPending && (
            <div className="flex items-center gap-2 py-3 text-sm text-slate-500">
              <span className="inline-block size-3 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
              <p role="status">Loading connected records…</p>
            </div>
          )}

          {data && (
            <div className="space-y-4">
              {/* Current Record Details */}
              <div className="rounded-xl border border-slate-200/90 bg-slate-50/50 p-4">
                {(() => {
                  const hasHumanCaption = Boolean(
                    currentRecordDisplay?.caption &&
                    currentRecordDisplay.caption !== data.current.object_id &&
                    !currentRecordDisplay.caption.startsWith("production-replay:")
                  );

                  return (
                    <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200/70 pb-2.5 mb-3.5">
                      <div className="flex items-center gap-2 flex-wrap">
                        <Database className="size-4 text-slate-600" />
                        <h3 className="font-semibold text-sm text-slate-900">
                          <span className="capitalize">{data.table}</span>
                          {hasHumanCaption ? (
                            <>
                              {" · "}
                              <span className="font-bold text-slate-900">{currentRecordDisplay?.caption}</span>
                            </>
                          ) : (
                            <span className="text-slate-500 font-normal"> Record</span>
                          )}
                        </h3>
                      </div>

                    </div>
                  );
                })()}

                {/* Display Fields */}
                {(() => {
                  const tableCols = displays.allColumns[data.table] ?? [];
                  const displayCols = tableCols;

                  if (displayCols.length > 0 && currentRecordDisplay?.values) {
                    return (
                      <div className="grid gap-2 text-sm sm:grid-cols-2 md:grid-cols-3">
                        {displayCols.map((col) => (
                          <div
                            key={col.id}
                            className="rounded-lg border border-slate-200/70 bg-white p-2.5 shadow-2xs hover:border-slate-300 transition-colors"
                          >
                            <dt className="text-xs font-semibold text-slate-400 uppercase tracking-wider mb-1">
                              {col.label}
                            </dt>
                            <dd className="font-semibold text-slate-800 break-words">
                              {displayValue(currentRecordDisplay.values[col.id])}
                            </dd>
                          </div>
                        ))}
                      </div>
                    );
                  }

                  // Fallback only if no configured display fields exist: filter out internal/technical IDs
                  const filteredEntries = Object.entries(data.current.data).filter(([key, val]) => {
                    const lk = key.toLowerCase();
                    if (
                      lk === "id" ||
                      lk.endsWith("_id") ||
                      lk === "object_id" ||
                      lk === "stream_id" ||
                      lk === "transaction_id" ||
                      lk === "tenant_id"
                    ) {
                      return false;
                    }
                    if (typeof val === "string" && val.startsWith("production-replay:")) {
                      return false;
                    }
                    return true;
                  });

                  return (
                    <dl className="grid gap-2 text-sm sm:grid-cols-2 md:grid-cols-3">
                      {filteredEntries.map(([key, value]) => (
                        <div
                          key={key}
                          className="rounded-lg border border-slate-200/60 bg-white p-2.5 hover:border-slate-300 transition-colors"
                        >
                          <dt className="text-xs font-semibold text-slate-400 uppercase tracking-wider mb-1">
                            {readable(key)}
                          </dt>
                          <dd className="break-all font-medium text-slate-800">
                            <ValueDisplay value={value} fieldKey={key} />
                          </dd>
                        </div>
                      ))}
                    </dl>
                  );
                })()}
              </div>

              {/* Connected Relationships */}
              <div className="space-y-3 rounded-xl border border-slate-200 bg-white p-4">
                <label className="block text-sm font-semibold uppercase tracking-wider text-slate-600">
                  Follow Connection
                  <select
                    className="mt-1.5 block w-full rounded-lg border border-slate-300 bg-white p-2 text-sm font-normal text-slate-800 shadow-2xs focus:border-blue-500 focus:outline-hidden"
                    value={connection}
                    onChange={(e) => {
                      setConnection(e.target.value);
                      setAfter("");
                    }}
                  >
                    <option value="">Choose a relationship</option>
                    {data.connections.map((e) => (
                      <option key={e.id} value={e.id}>
                        {e.label} → {e.target}
                      </option>
                    ))}
                  </select>
                </label>

                {!data.connections.length && (
                  <p className="text-sm text-slate-500 italic">
                    No active configured links, pivots or navigation options for this record.
                  </p>
                )}

                {selected && (
                  <div className="space-y-3 rounded-lg bg-blue-50/50 border border-blue-100 p-3 mt-2">
                    <p className="text-sm font-medium text-blue-900">
                      Connected because: {selected.reason}
                    </p>

                    <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                      {data.records.map((record) => {
                        const recDisplay = selected
                          ? displays.record({ object_type: selected.target, object_id: record.object_id })
                          : undefined;
                        const caption =
                          recDisplay?.caption && recDisplay.caption !== record.object_id
                            ? recDisplay.caption
                            : null;
                        const targetCols = selected
                          ? (displays.allColumns[selected.target] ?? []).slice(0, 3)
                          : [];

                        return (
                          <li key={record.object_id}>
                            <button
                              type="button"
                              disabled={path.length >= 6}
                              onClick={() =>
                                navigate([
                                  ...path,
                                  {
                                    connection,
                                    object_id: record.object_id,
                                    label: selected.target,
                                    relationshipLabel: selected.label,
                                  },
                                ])
                              }
                              className="w-full text-left rounded-lg border border-blue-200 bg-white p-2.5 hover:bg-blue-50/70 hover:border-blue-300 transition-all shadow-2xs group cursor-pointer disabled:opacity-50"
                            >
                              <div className="flex items-center justify-between gap-1.5">
                                <span className="text-xs font-bold text-blue-900 truncate">
                                  {caption || (record.object_id.startsWith("production-replay:") ? selected.target : record.object_id)}
                                </span>
                                <ArrowRight className="size-3 text-blue-400 group-hover:text-blue-700 transition-colors shrink-0" />
                              </div>
                              {targetCols.length > 0 && recDisplay?.values && (
                                <div className="mt-1.5 pt-1.5 border-t border-slate-100 flex flex-wrap gap-1 text-[11px]">
                                  {targetCols.map((c) => (
                                    <span
                                      key={c.id}
                                      className="text-slate-600 bg-slate-50 px-1.5 py-0.5 rounded border border-slate-200/60"
                                    >
                                      <span className="text-slate-400">{c.label}:</span>{" "}
                                      <span className="font-medium text-slate-800">
                                        {displayValue(recDisplay.values[c.id])}
                                      </span>
                                    </span>
                                  ))}
                                </div>
                              )}
                            </button>
                          </li>
                        );
                      })}
                    </ul>

                    {!data.records.length && (
                      <p className="text-sm text-slate-500">
                        No matching records on this page. Empty relationship values do not match missing values.
                      </p>
                    )}

                    <div className="flex gap-2 pt-1">
                      {after && (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setAfter("")}
                          className="text-sm"
                        >
                          First records
                        </Button>
                      )}
                      {data.next_after && (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setAfter(data.next_after)}
                          className="text-sm"
                        >
                          More connected records
                        </Button>
                      )}
                    </div>
                  </div>
                )}

                {path.length >= 6 && (
                  <p className="rounded-md bg-amber-50 p-2 text-sm text-amber-800 border border-amber-200">
                    Six-step exploration limit reached. Use the trail to return to an earlier record.
                  </p>
                )}
              </div>

              {/* Associated Decisions */}
              <section
                aria-label="Associated decisions"
                className="space-y-3 rounded-xl border border-slate-200 bg-white p-4"
              >
                <div>
                  <h4 className="text-sm font-semibold text-slate-900">Decisions for this record</h4>
                  <p className="text-sm text-slate-500 mt-0.5">
                    Newest first. Decisions belonging to cases outside your access are omitted. Discovery does not attach them automatically.
                  </p>
                </div>

                {!data.decisions?.length && (
                  <p className="text-sm text-slate-500 italic py-2">
                    No accessible decisions on this page.
                  </p>
                )}

                <ul className="space-y-2.5">
                  {data.decisions?.map((d) => (
                    <li
                      key={d.id}
                      className="rounded-lg border border-slate-200 p-3 text-sm bg-slate-50/40 hover:bg-slate-50 transition-colors space-y-2"
                    >
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="flex items-center gap-2">
                          <span className="font-semibold text-slate-900">
                            {d.scenario_name} — {readable(d.outcome)}
                          </span>
                        </div>
                        <StatusPill value={d.outcome} />
                      </div>

                      <div className="flex flex-wrap items-center gap-3 text-xs text-slate-600">
                        <span>Score: <strong className="text-slate-900">{d.score}</strong></span>
                        <span>•</span>
                        <span>Triggered: <strong>{d.triggered ? "Yes" : "No"}</strong></span>
                        <span>•</span>
                        <span className="text-slate-400">{new Date(d.created_at).toLocaleString()}</span>
                      </div>

                      {d.case ? (
                        <div className="flex flex-wrap items-center gap-2 rounded bg-white p-2 border border-slate-200/80 text-sm text-slate-700">
                          <span>
                            {d.case.id === caseId ? "Already in this case: " : "Case: "}
                          </span>
                          <Link
                            className="font-medium text-blue-700 hover:underline"
                            href={`/cases/${d.case.id}?${new URLSearchParams({ tenant })}`}
                          >
                            {d.case.name}
                          </Link>
                          <span>·</span>
                          <span className="capitalize">{readable(d.case.status)}</span>
                          <Link
                            className="ml-auto text-blue-700 hover:underline inline-flex items-center gap-1"
                            href={`/cases/decisions/${d.id}?${new URLSearchParams({ tenant })}`}
                          >
                            Open decision investigation
                            <ArrowRight className="size-3" />
                          </Link>
                        </div>
                      ) : (
                        <p className="text-sm text-slate-400 italic">Not attached to a case.</p>
                      )}

                      {d.can_attach && (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={!!attaching}
                          onClick={() => void attachDecision(d)}
                          className="text-sm gap-1 border-blue-200 text-blue-700 hover:bg-blue-50"
                        >
                          {attaching === d.id ? "Attaching…" : "Attach decision to this case"}
                        </Button>
                      )}
                    </li>
                  ))}
                </ul>

                <div className="flex gap-2 pt-1">
                  {decisionAfter && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setDecisionAfter("")}
                      className="text-sm"
                    >
                      Newest decisions
                    </Button>
                  )}
                  {data.next_decision_after && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setDecisionAfter(data.next_decision_after)}
                      className="text-sm"
                    >
                      Older decisions
                    </Button>
                  )}
                </div>
              </section>

              <div className="pt-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={saving}
                  onClick={() => void saveTrail()}
                  className="gap-1.5 text-sm font-medium border-slate-300 hover:bg-slate-50"
                >
                  <Bookmark className="size-3.5 text-slate-600" />
                  Save connection trail to case notes
                </Button>
              </div>
            </div>
          )}

          <Problem error={saveError} />
          {notice && (
            <p
              role="status"
              className="rounded-lg bg-emerald-50 p-2.5 text-sm font-medium text-emerald-800 border border-emerald-200"
            >
              {notice}
            </p>
          )}
        </section>
      )}
    </div>
  );
}
