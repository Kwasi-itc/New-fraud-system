"use client";

import { useState } from "react";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  ExternalLink,
  Layers,
  List,
  Search,
  Table as TableIcon,
  Code2,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { readable } from "./shared";

interface StructuredDataViewerProps {
  data: unknown;
  title?: string;
  className?: string;
  defaultExpanded?: boolean;
  maxInitialDepth?: number;
}

export function CopyButton({ text, className }: { text: string; className?: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Fallback or ignore
    }
  };

  return (
    <button
      type="button"
      onClick={handleCopy}
      title={copied ? "Copied!" : "Copy to clipboard"}
      className={cn(
        "inline-flex items-center gap-1 rounded p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-700 transition-colors",
        className
      )}
    >
      {copied ? <Check className="size-3.5 text-emerald-600" /> : <Copy className="size-3.5" />}
      <span className="sr-only">Copy</span>
    </button>
  );
}

export function StatusPill({ value }: { value: string }) {
  const normalized = value.toLowerCase().trim();

  let style = "bg-slate-100 text-slate-700 border-slate-200";

  if (["approved", "pass", "passed", "success", "confirmed", "true", "active", "completed"].includes(normalized)) {
    style = "bg-emerald-50 text-emerald-700 border-emerald-200";
  } else if (["declined", "rejected", "fail", "failed", "high", "confirmed_risk", "false"].includes(normalized)) {
    style = "bg-rose-50 text-rose-700 border-rose-200";
  } else if (["pending", "review", "investigating", "medium", "warn", "warning", "valuable_alert"].includes(normalized)) {
    style = "bg-amber-50 text-amber-700 border-amber-200";
  } else if (["closed", "low", "false_positive"].includes(normalized)) {
    style = "bg-blue-50 text-blue-700 border-blue-200";
  }

  return (
    <span className={cn("inline-flex items-center rounded-full border px-2.5 py-0.5 text-sm font-medium capitalize", style)}>
      {readable(value)}
    </span>
  );
}

function isDateString(value: string): boolean {
  if (typeof value !== "string" || value.length < 10) return false;
  if (!/^\d{4}-\d{2}-\d{2}/.test(value)) return false;
  const time = Date.parse(value);
  return Number.isFinite(time);
}

function isUuid(value: string): boolean {
  return typeof value === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value);
}

export function ValueDisplay({ value, fieldKey }: { value: unknown; fieldKey?: string }) {
  if (value === null || value === undefined) {
    return <span className="inline-block text-sm font-normal italic text-slate-400">Not recorded</span>;
  }

  if (typeof value === "boolean") {
    return (
      <span
        className={cn(
          "inline-flex items-center rounded-md px-2.5 py-0.5 text-sm font-semibold",
          value ? "bg-emerald-50 text-emerald-700 border border-emerald-200" : "bg-slate-100 text-slate-600 border border-slate-200"
        )}
      >
        {value ? "True" : "False"}
      </span>
    );
  }

  if (typeof value === "number") {
    return (
      <span className="font-mono text-sm font-medium text-slate-800">
        {String(value)}
      </span>
    );
  }

  if (typeof value === "string") {
    // Check if it's an ISO timestamp
    if (isDateString(value)) {
      const date = new Date(value);
      return (
        <span className="inline-flex items-center gap-2 text-sm text-slate-700" title={value}>
          <span className="font-medium">{date.toLocaleDateString()}</span>
          <span className="text-slate-400 font-mono text-sm">{date.toLocaleTimeString()}</span>
        </span>
      );
    }

    // Check if UUID
    if (isUuid(value)) {
      return (
        <span className="group inline-flex items-center gap-1.5 rounded bg-slate-100/90 px-2 py-0.5 font-mono text-sm text-slate-700">
          <span className="truncate max-w-[240px]" title={value}>{value}</span>
          <CopyButton text={value} />
        </span>
      );
    }

    // Check if URL
    if (value.startsWith("http://") || value.startsWith("https://")) {
      return (
        <a
          href={value}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 text-sm text-blue-700 hover:underline break-all"
        >
          {value}
          <ExternalLink className="size-3.5 shrink-0" />
        </a>
      );
    }

    // Status or outcome keywords
    if (fieldKey && /(status|outcome|level|state|result|lifecycle)/i.test(fieldKey) && value.length < 30) {
      return <StatusPill value={value} />;
    }

    return <span className="break-words text-sm text-slate-800">{value}</span>;
  }

  // Nested object or array
  return <StructuredNode value={value} depth={1} />;
}

function ArrayViewer({ items, depth }: { items: unknown[]; depth: number }) {
  const [viewMode, setViewMode] = useState<"cards" | "table">("cards");

  if (items.length === 0) {
    return <span className="text-sm italic text-slate-400">Empty list</span>;
  }

  // Check if uniform array of objects (suitable for table)
  const isUniformObjects =
    items.length > 0 &&
    items.every(
      (item) => item !== null && typeof item === "object" && !Array.isArray(item)
    );

  const sampleKeys = isUniformObjects
    ? Object.keys(items[0] as Record<string, unknown>).slice(0, 8)
    : [];

  const canShowTable = isUniformObjects && sampleKeys.length > 0 && sampleKeys.length <= 10;

  return (
    <div className="space-y-3 mt-1.5">
      {canShowTable && (
        <div className="flex justify-end gap-1.5 mb-1.5">
          <button
            type="button"
            onClick={() => setViewMode("cards")}
            className={cn(
              "px-2.5 py-1 rounded text-sm flex items-center gap-1.5 border transition-colors",
              viewMode === "cards" ? "bg-slate-200 text-slate-900 border-slate-300 font-medium" : "bg-white text-slate-600 border-slate-200"
            )}
          >
            <List className="size-3.5" /> Cards
          </button>
          <button
            type="button"
            onClick={() => setViewMode("table")}
            className={cn(
              "px-2.5 py-1 rounded text-sm flex items-center gap-1.5 border transition-colors",
              viewMode === "table" ? "bg-slate-200 text-slate-900 border-slate-300 font-medium" : "bg-white text-slate-600 border-slate-200"
            )}
          >
            <TableIcon className="size-3.5" /> Table
          </button>
        </div>
      )}

      {viewMode === "table" && canShowTable ? (
        <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white">
          <table className="w-full text-left text-sm">
            <thead className="bg-slate-50 border-b border-slate-200 text-slate-600 font-medium">
              <tr>
                <th className="px-3.5 py-2.5 text-sm uppercase tracking-wider text-slate-500 font-semibold w-12">#</th>
                {sampleKeys.map((key) => (
                  <th key={key} className="px-3.5 py-2.5 font-medium">
                    {readable(key)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {items.map((row, idx) => {
                const rowObj = row as Record<string, unknown>;
                return (
                  <tr key={idx} className="hover:bg-slate-50/80 transition-colors">
                    <td className="px-3.5 py-2.5 font-mono text-sm text-slate-400">{idx + 1}</td>
                    {sampleKeys.map((key) => (
                      <td key={key} className="px-3.5 py-2.5">
                        <ValueDisplay value={rowObj[key]} fieldKey={key} />
                      </td>
                    ))}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="space-y-2.5">
          {items.map((item, index) => (
            <div
              key={index}
              className="rounded-lg border border-slate-200 bg-white p-3.5 shadow-xs hover:border-slate-300 transition-colors"
            >
              <div className="flex items-center gap-2 mb-2.5 text-sm font-semibold text-slate-600 pb-2 border-b border-slate-100">
                <span className="flex items-center justify-center size-5 rounded-full bg-slate-100 text-sm font-bold text-slate-700">
                  {index + 1}
                </span>
                <span>Item {index + 1}</span>
              </div>
              <StructuredNode value={item} depth={depth + 1} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function ObjectViewer({ obj, depth }: { obj: Record<string, unknown>; depth: number }) {
  const [filter, setFilter] = useState("");
  const entries = Object.entries(obj);

  if (entries.length === 0) {
    return <span className="text-sm italic text-slate-400">Empty object</span>;
  }

  const filtered = filter
    ? entries.filter(([k]) => k.toLowerCase().includes(filter.toLowerCase()))
    : entries;

  return (
    <div className="space-y-2.5">
      {entries.length > 8 && depth <= 2 && (
        <div className="relative mb-2.5">
          <Search className="absolute left-3 top-2.5 size-3.5 text-slate-400" />
          <input
            type="text"
            placeholder="Filter fields..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="w-full rounded-md border border-slate-200 bg-white py-2 pl-9 pr-3 text-sm placeholder:text-slate-400 focus:border-blue-500 focus:outline-hidden"
          />
        </div>
      )}

      <div className="grid gap-2 sm:grid-cols-1">
        {filtered.map(([key, val]) => {
          const isComplex = val !== null && typeof val === "object";

          if (isComplex) {
            return (
              <CollapsibleField key={key} fieldKey={key} value={val} depth={depth + 1} />
            );
          }

          return (
            <div
              key={key}
              className="grid grid-cols-1 sm:grid-cols-[200px_1fr] items-start gap-2 py-2 px-2.5 rounded-md hover:bg-slate-50/80 border-b border-slate-100/70 last:border-0 transition-colors"
            >
              <span className="text-sm font-medium text-slate-600 capitalize select-all">
                {readable(key)}
              </span>
              <div className="min-w-0">
                <ValueDisplay value={val} fieldKey={key} />
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function CollapsibleField({
  fieldKey,
  value,
  depth,
}: {
  fieldKey: string;
  value: unknown;
  depth: number;
}) {
  const [expanded, setExpanded] = useState(depth <= 2);
  const isArray = Array.isArray(value);
  const itemCount = isArray ? value.length : Object.keys((value as Record<string, unknown>) || {}).length;

  return (
    <div className="rounded-lg border border-slate-200/90 bg-white overflow-hidden mt-2 mb-2">
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center justify-between px-3.5 py-2.5 text-left bg-slate-50/70 hover:bg-slate-100/70 transition-colors"
      >
        <div className="flex items-center gap-2">
          {expanded ? (
            <ChevronDown className="size-4 text-slate-400" />
          ) : (
            <ChevronRight className="size-4 text-slate-400" />
          )}
          <span className="text-sm font-semibold text-slate-800 capitalize">
            {readable(fieldKey)}
          </span>
        </div>
        <span className="text-sm font-semibold text-slate-500 bg-white px-2.5 py-0.5 rounded-full border border-slate-200">
          {itemCount} {isArray ? "items" : "fields"}
        </span>
      </button>

      {expanded && (
        <div className="p-3.5 border-t border-slate-100 bg-white">
          <StructuredNode value={value} depth={depth} />
        </div>
      )}
    </div>
  );
}

function StructuredNode({ value, depth = 0 }: { value: unknown; depth?: number }) {
  if (value === null || value === undefined) {
    return <span className="text-sm italic text-slate-400">Not recorded</span>;
  }

  if (typeof value !== "object") {
    return <ValueDisplay value={value} />;
  }

  if (Array.isArray(value)) {
    return <ArrayViewer items={value} depth={depth} />;
  }

  return <ObjectViewer obj={value as Record<string, unknown>} depth={depth} />;
}

export function StructuredDataViewer({
  data,
  title,
  className,
}: StructuredDataViewerProps) {
  const [rawView, setRawView] = useState(false);

  if (data === null || data === undefined) {
    return <p className="text-sm italic text-slate-400">No data available.</p>;
  }

  const rawJson = JSON.stringify(data, null, 2);

  // Check if data contains decision evaluation signals
  const isDecisionData = typeof data === "object" && data !== null && ("outcome" in data || "score" in data || "triggered_rules" in data);
  const decisionObj = isDecisionData ? (data as Record<string, unknown>) : null;
  const triggeredRules = Array.isArray(decisionObj?.triggered_rules) ? (decisionObj.triggered_rules as string[]) : [];

  return (
    <div className={cn("rounded-xl border border-slate-200 bg-slate-50/50 p-4", className)}>
      <div className="flex items-center justify-between gap-3 mb-3.5 pb-2.5 border-b border-slate-200/80">
        <div className="flex items-center gap-2">
          <Layers className="size-4 text-blue-600" />
          <h4 className="text-sm font-semibold uppercase tracking-wider text-slate-700">
            {title ?? "Evidence & Attributes"}
          </h4>
        </div>
        <div className="flex items-center gap-2">
          <CopyButton text={rawJson} />
          <button
            type="button"
            onClick={() => setRawView(!rawView)}
            className="flex items-center gap-1.5 text-xs font-semibold text-slate-700 hover:text-slate-900 bg-white border border-slate-200 px-2.5 py-1 rounded-lg shadow-2xs hover:bg-slate-50 transition-colors cursor-pointer"
          >
            <Code2 className="size-3.5" />
            {rawView ? "Visual view" : "Raw JSON"}
          </button>
        </div>
      </div>

      {rawView ? (
        <div className="relative rounded-lg border border-slate-200 bg-slate-900 p-4 overflow-x-auto text-sm text-slate-100 font-mono">
          <pre className="whitespace-pre-wrap break-all leading-relaxed text-xs selection:bg-blue-500/30">
            {rawJson}
          </pre>
        </div>
      ) : (
        <div className="space-y-4">
          {/* Executive Risk & Rule Evaluation Bar when viewing Decision Evidence */}
          {decisionObj && (
            <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-2xs space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-center gap-3">
                  <div>
                    <span className="text-xs uppercase tracking-wider text-slate-400 font-semibold block">Decision Outcome</span>
                    <div className="mt-0.5">
                      <StatusPill value={String(decisionObj.outcome ?? "unknown")} />
                    </div>
                  </div>
                  {typeof decisionObj.score === "number" && (
                    <div className="border-l border-slate-200 pl-3">
                      <span className="text-xs uppercase tracking-wider text-slate-400 font-semibold block">Risk Score</span>
                      <span className="font-mono text-base font-bold text-slate-900 mt-0.5 block">{decisionObj.score}</span>
                    </div>
                  )}
                  {!!decisionObj.scenario && (
                    <div className="border-l border-slate-200 pl-3">
                      <span className="text-xs uppercase tracking-wider text-slate-400 font-semibold block">Scenario</span>
                      <span className="text-sm font-semibold text-slate-800 mt-0.5 block">{String(decisionObj.scenario)}</span>
                    </div>
                  )}
                </div>
              </div>

              {triggeredRules.length > 0 && (
                <div className="pt-2 border-t border-slate-100">
                  <span className="text-xs uppercase tracking-wider text-rose-700 font-semibold block mb-1.5">
                    Triggered Rules ({triggeredRules.length})
                  </span>
                  <div className="flex flex-wrap gap-1.5">
                    {triggeredRules.map((ruleName, idx) => (
                      <span
                        key={idx}
                        className="inline-flex items-center rounded-md bg-rose-50 px-2.5 py-1 text-xs font-semibold text-rose-700 border border-rose-200"
                      >
                        {ruleName}
                      </span>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}

          <StructuredNode value={data} depth={0} />
        </div>
      )}
    </div>
  );
}
