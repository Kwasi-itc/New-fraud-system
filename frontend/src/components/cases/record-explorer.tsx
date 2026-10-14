"use client";

import { useState } from "react";
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
import { Problem, readable } from "./shared";
import { StatusPill, ValueDisplay } from "./structured-data-viewer";

type Hop = { connection: string; object_id: string; label: string };
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
      const trailSteps = path
        .map((h, i) => `  ${i + 1}. [${h.connection}] ${h.label}: ${h.object_id}`)
        .join("\n");
      const commentBody = [
        "### Investigation Connection Trail",
        `• **Decision ID**: ${decisionId}`,
        `• **Terminal Record**: \`${data.table}\` / \`${data.current.object_id}\``,
        `• **Observed**: ${new Date().toISOString()}`,
        "",
        "**Exploration Path:**",
        trailSteps || "  (Root entity direct inspection)",
        "",
        "_Note: Live graph traversal record; not a static evidence snapshot._",
      ].join("\n");

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
            <p className="mt-1 text-xs text-slate-500">
              Traverse live configured entity connections. Discovered records show live transactional state rather than immutable evidence snapshots.
            </p>
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
            {path.map((hop, index) => (
              <span key={index} className="flex items-center gap-1.5">
                <ChevronRight className="size-3 text-slate-400" />
                <button
                  type="button"
                  className={
                    index === path.length - 1
                      ? "rounded px-2.5 py-1 font-bold text-blue-700 bg-white shadow-2xs border border-blue-200 cursor-pointer"
                      : "rounded px-2.5 py-1 text-slate-600 hover:bg-white hover:text-slate-900 transition-colors cursor-pointer"
                  }
                  onClick={() => navigate(path.slice(0, index + 1))}
                >
                  <span className="capitalize">{hop.label}</span>: <span className="font-mono">{hop.object_id}</span>
                </button>
              </span>
            ))}
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
                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200/70 pb-2 mb-3">
                  <div className="flex items-center gap-2">
                    <Database className="size-4 text-slate-600" />
                    <h3 className="font-semibold text-sm text-slate-900">
                      {data.table}: {data.current.object_id}
                    </h3>
                  </div>
                  <span className="text-sm font-medium text-slate-500 bg-white px-2 py-0.5 rounded-full border border-slate-200">
                    Live Record
                  </span>
                </div>

                {/* Attributes grid without raw JSON */}
                <dl className="grid gap-2 text-sm sm:grid-cols-2">
                  {Object.entries(data.current.data).map(([key, value]) => (
                    <div
                      key={key}
                      className="rounded-lg border border-slate-200/60 bg-white p-2.5 hover:border-slate-300 transition-colors"
                    >
                      <dt className="text-sm font-medium text-slate-400 uppercase tracking-wider mb-1">
                        {readable(key)}
                      </dt>
                      <dd className="break-all font-medium text-slate-800">
                        <ValueDisplay value={value} fieldKey={key} />
                      </dd>
                    </div>
                  ))}
                </dl>
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

                    <ul className="flex flex-wrap gap-2">
                      {data.records.map((record) => (
                        <li key={record.object_id}>
                          <Button
                            variant="outline"
                            size="sm"
                            disabled={path.length >= 6}
                            onClick={() =>
                              navigate([
                                ...path,
                                {
                                  connection,
                                  object_id: record.object_id,
                                  label: selected.target,
                                },
                              ])
                            }
                            className="text-sm bg-white border-blue-200 hover:bg-blue-50 text-blue-800"
                          >
                            Open {selected.target}: {record.object_id}
                          </Button>
                        </li>
                      ))}
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

                      <div className="flex flex-wrap items-center gap-3 text-slate-600">
                        <span className="font-mono text-sm text-slate-500">
                          Decision {d.id}
                        </span>
                        <span>•</span>
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
