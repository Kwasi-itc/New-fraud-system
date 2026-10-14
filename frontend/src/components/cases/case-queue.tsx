"use client";

import Link from "next/link";
import { BulkActionsModal } from "./bulk-actions";
import { SelectPageCheckbox } from "./decision-selection";
import { AssigneeSearchDropdown } from "./assignee-search-dropdown";
import { useEffect, useState } from "react";
import {
  ChevronDown,
  Clock,
  Layers,
  Plus,
  RotateCcw,
  SlidersHorizontal,
  User,
} from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  useCaseContext,
  useCaseQueue,
  useCaseWrite,
} from "@/lib/case-manager-query";
import type { CaseRecord } from "@/lib/case-manager-api";
import {
  CaseNav,
  Panel,
  Problem,
  fieldClass,
  labelClass,
  readable,
  timestamp,
} from "./shared";
import { StatusPill } from "./structured-data-viewer";
import { cn } from "@/lib/utils";

export function CaseQueue({
  tenant,
  session,
}: {
  tenant: string;
  session: string;
}) {
  const router = useRouter();
  const location = useSearchParams();
  const { identity, inboxes, tags } = useCaseContext(tenant, session);
  const allowed = [
    "name",
    "inbox_id",
    "tag_id",
    "review_level",
    "assignee_id",
    "status",
    "unassigned",
    "include_snoozed",
    "overdue",
    "created_from",
    "created_to",
  ];
  const active = new URLSearchParams(
    location.get("queue") ?? "status=pending&status=investigating"
  );
  for (const key of [...active.keys()]) if (!allowed.includes(key)) active.delete(key);
  const filters = active.toString();
  const setFilters = (value: string) =>
    router.push(`/cases?${new URLSearchParams({ tenant, queue: value })}`, {
      scroll: false,
    });
  const statuses = active.getAll("status");
  const lifecycle =
    statuses.length === 2 &&
    statuses.includes("pending") &&
    statuses.includes("investigating")
      ? "active"
      : statuses.length === 1
      ? statuses[0]
      : statuses.length
      ? "selected"
      : "";
  const dateValue = (key: string) => {
    const value = active.get(key);
    return value && Number.isFinite(Date.parse(value))
      ? new Date(value).toISOString().slice(0, 16)
      : "";
  };
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [selectedCaseIds, setSelectedCaseIds] = useState<string[]>([]);
  const [bulkModalOpen, setBulkModalOpen] = useState(false);
  const filterCount = new Set(active.keys()).size;
  const queue = useCaseQueue(tenant, session, filters, identity.isSuccess);
  const write = useCaseWrite(tenant, session);
  const items = queue.data?.pages.flatMap((page) => page.cases) ?? [];
  const counts =
    queue.error || identity.error ? undefined : queue.data?.pages[0]?.counts;
  const knownUsers = Array.from(
    new Set([
      identity.data?.subject,
      ...items.map((i) => i.assigned_to),
      ...items.flatMap((i) => i.contributors?.map((c) => c.user_id) ?? []),
    ].filter(Boolean) as string[])
  );

  useEffect(() => {
    setSelectedCaseIds([]);
  }, [filters]);

  const toggleCase = (id: string) => {
    setSelectedCaseIds((prev) =>
      prev.includes(id) ? prev.filter((item) => item !== id) : [...prev, id]
    );
  };

  const toggleAll = () => {
    setSelectedCaseIds((prev) =>
      prev.length === items.length ? [] : items.map((item) => item.id)
    );
  };

  return (
    <div className="space-y-3">
      <CaseNav tenant={tenant} />

      {/* Compact Page Header with Status Counters */}
      {/* Compact Page Header */}
      <div className="flex flex-wrap items-center justify-between gap-3 py-1">
        <div>
          <h1 className="text-xl sm:text-2xl font-bold tracking-tight text-slate-900">
            Cases
          </h1>
          <p className="text-sm text-slate-500">
            Investigate alerts, record findings, and resolve cases.
          </p>
        </div>

        {/* Small Metric Cards */}
        {counts && (
          <div className="grid grid-cols-3 gap-2.5 sm:gap-3">
            <div className="rounded-xl border border-slate-200/90 bg-white px-3.5 py-2 shadow-2xs min-w-[100px]">
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-semibold uppercase tracking-wider text-slate-500">
                  Pending
                </span>
                <span className="size-2 rounded-full bg-amber-500 shrink-0" />
              </div>
              <p className="mt-1 text-lg font-bold tracking-tight text-slate-900">
                {counts.pending ?? 0}
              </p>
            </div>

            <div className="rounded-xl border border-slate-200/90 bg-white px-3.5 py-2 shadow-2xs min-w-[100px]">
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-semibold uppercase tracking-wider text-slate-500">
                  Investigating
                </span>
                <span className="size-2 rounded-full bg-blue-500 shrink-0" />
              </div>
              <p className="mt-1 text-lg font-bold tracking-tight text-slate-900">
                {counts.investigating ?? 0}
              </p>
            </div>

            <div className="rounded-xl border border-slate-200/90 bg-white px-3.5 py-2 shadow-2xs min-w-[100px]">
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-semibold uppercase tracking-wider text-slate-500">
                  Closed
                </span>
                <span className="size-2 rounded-full bg-slate-400 shrink-0" />
              </div>
              <p className="mt-1 text-lg font-bold tracking-tight text-slate-900">
                {counts.closed ?? 0}
              </p>
            </div>
          </div>
        )}
      </div>

      {!tenant && (
        <div className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
          Your account needs an organization assignment to load cases.
        </div>
      )}

      <Problem error={identity.error} retry={() => void identity.refetch()} />
      <Problem
        error={inboxes.error ?? tags.error}
        retry={() => {
          void inboxes.refetch();
          void tags.refetch();
        }}
      />

      {/* Main Queue Container */}
      <div className="w-full">
        <Panel title="Inbox queue">
          {/* Filter Controls Header */}
          <div className="flex flex-wrap items-center justify-between gap-3 pb-2">
            <div className="flex flex-wrap items-center gap-3">
              <Button
                variant="outline"
                aria-label="Filters"
                aria-expanded={filtersOpen}
                aria-controls="case-queue-filters"
                onClick={() => setFiltersOpen(!filtersOpen)}
                className="h-10 gap-2 rounded-xl border-slate-200 bg-white px-4 text-sm font-semibold shadow-none"
              >
                <SlidersHorizontal className="size-4" aria-hidden="true" />
                Filters
                {filterCount > 0 && (
                  <span
                    className="rounded-full bg-blue-100 px-2 py-0.5 text-sm font-bold text-blue-700"
                    aria-label={`${filterCount} active filters`}
                  >
                    {filterCount}
                  </span>
                )}
                <ChevronDown
                  aria-hidden="true"
                  className={`size-4 transition-transform ${
                    filtersOpen ? "rotate-180" : ""
                  }`}
                />
              </Button>

              <Button
                disabled={!selectedCaseIds.length}
                onClick={() => setBulkModalOpen(true)}
                className="h-10 gap-2 rounded-xl bg-blue-600 px-4 text-sm font-semibold text-white shadow-none hover:bg-blue-700 disabled:bg-slate-100 disabled:text-slate-400"
              >
                <Layers className="size-4" />
                Bulk actions{selectedCaseIds.length ? ` (${selectedCaseIds.length})` : ""}
              </Button>

              {selectedCaseIds.length > 0 && (
                <Button
                  variant="ghost"
                  onClick={() => setSelectedCaseIds([])}
                  className="h-10 rounded-xl text-sm font-medium text-slate-600 hover:text-slate-900"
                >
                  Clear selection
                </Button>
              )}

              <span className="text-sm text-slate-600">
                {filterCount
                  ? `${filterCount} active filter${filterCount === 1 ? "" : "s"}`
                  : "All cases"}
              </span>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={!identity.isSuccess}
                onClick={() => {
                  const next = new URLSearchParams(filters);
                  next.delete("unassigned");
                  next.set("assignee_id", identity.data!.subject);
                  setFilters(next.toString());
                }}
                className="h-10 rounded-xl text-sm gap-1.5"
              >
                <User className="size-4 text-slate-400" />
                Assigned to me
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => setFilters("status=pending&status=investigating")}
                className="h-10 rounded-xl text-sm gap-1.5"
              >
                <RotateCcw className="size-4 text-slate-400" />
                Reset filters
              </Button>
            </div>
          </div>

            {/* Expandable Filter Form */}
            <div id="case-queue-filters" hidden={!filtersOpen}>
              <form
                key={filters}
                className="grid items-end gap-3 rounded-xl border border-slate-200 bg-slate-50/70 p-4 md:grid-cols-2 lg:grid-cols-3 mt-2"
                onSubmit={(event) => {
                  event.preventDefault();
                  const data = new FormData(event.currentTarget);
                  const query = new URLSearchParams();
                  for (const key of [
                    "name",
                    "inbox_id",
                    "tag_id",
                    "review_level",
                    "assignee_id",
                  ]) {
                    const value = String(data.get(key) ?? "").trim();
                    if (value) query.set(key, value);
                  }
                  const status = String(data.get("status"));
                  if (status === "active") {
                    query.append("status", "pending");
                    query.append("status", "investigating");
                  } else if (status === "selected") {
                    for (const value of statuses) query.append("status", value);
                  } else if (status) query.set("status", status);
                  for (const key of ["unassigned", "include_snoozed", "overdue"])
                    if (data.get(key)) query.set(key, "true");
                  for (const key of ["created_from", "created_to"]) {
                    const value = String(data.get(key) ?? "");
                    if (value) query.set(key, new Date(`${value}Z`).toISOString());
                  }
                  setFilters(query.toString());
                }}
              >
                <label className={labelClass}>
                  <span>Search</span>
                  <Input
                    name="name"
                    defaultValue={active.get("name") ?? ""}
                    placeholder="Case name"
                    className="font-normal normal-case text-sm"
                  />
                </label>
                <label className={labelClass}>
                  <span>Inbox</span>
                  <select
                    name="inbox_id"
                    defaultValue={active.get("inbox_id") ?? ""}
                    className={cn(fieldClass, "font-normal normal-case text-sm")}
                  >
                    <option value="">All permitted inboxes</option>
                    {inboxes.data?.inboxes.map((inbox) => (
                      <option key={inbox.id} value={inbox.id}>
                        {inbox.name}
                        {inbox.status === "archived" ? " (archived)" : ""}
                      </option>
                    ))}
                  </select>
                </label>
                <label className={labelClass}>
                  <span>Lifecycle</span>
                  <select
                    name="status"
                    defaultValue={lifecycle}
                    className={cn(fieldClass, "font-normal normal-case text-sm")}
                  >
                    {lifecycle === "selected" && (
                      <option value="selected">Selected statuses</option>
                    )}
                    <option value="active">Open cases</option>
                    <option value="">All statuses</option>
                    <option value="pending">Pending</option>
                    <option value="investigating">Investigating</option>
                    <option value="closed">Closed</option>
                  </select>
                </label>
                <label className={labelClass}>
                  <span>Tag</span>
                  <select
                    name="tag_id"
                    defaultValue={active.get("tag_id") ?? ""}
                    className={cn(fieldClass, "font-normal normal-case text-sm")}
                  >
                    <option value="">Any tag</option>
                    {tags.data?.tags.map((tag) => (
                      <option key={tag.id} value={tag.id}>
                        {tag.name}
                      </option>
                    ))}
                  </select>
                </label>
                <label className={labelClass}>
                  <span>Review level</span>
                  <select
                    name="review_level"
                    defaultValue={active.get("review_level") ?? ""}
                    className={cn(fieldClass, "font-normal normal-case text-sm")}
                  >
                    <option value="">Any review level</option>
                    {["probable_false_positive", "investigate", "escalate"].map(
                      (v) => (
                        <option key={v} value={v}>
                          {readable(v)}
                        </option>
                      )
                    )}
                  </select>
                </label>
                <AssigneeSearchDropdown
                  label="Assignee ID"
                  name="assignee_id"
                  defaultValue={active.get("assignee_id") ?? ""}
                  currentUserId={identity.data?.subject}
                  users={knownUsers}
                />
                <label className={labelClass}>
                  <span>Created from (UTC)</span>
                  <Input
                    name="created_from"
                    defaultValue={dateValue("created_from")}
                    type="datetime-local"
                    className="font-normal normal-case text-sm"
                  />
                </label>
                <label className={labelClass}>
                  <span>Created before (UTC)</span>
                  <Input
                    name="created_to"
                    defaultValue={dateValue("created_to")}
                    type="datetime-local"
                    className="font-normal normal-case text-sm"
                  />
                </label>
                <div className="flex flex-wrap gap-4 text-sm font-medium text-slate-700 md:col-span-2 pt-2">
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      name="unassigned"
                      type="checkbox"
                      defaultChecked={active.get("unassigned") === "true"}
                      className="rounded border-slate-300 text-blue-600 focus:ring-blue-500"
                    />
                    Unassigned only
                  </label>
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      name="include_snoozed"
                      type="checkbox"
                      defaultChecked={active.get("include_snoozed") === "true"}
                      className="rounded border-slate-300 text-blue-600 focus:ring-blue-500"
                    />
                    Include snoozed
                  </label>
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      name="overdue"
                      type="checkbox"
                      defaultChecked={active.get("overdue") === "true"}
                      className="rounded border-slate-300 text-blue-600 focus:ring-blue-500"
                    />
                    Overdue SLA only
                  </label>
                </div>
                <div className="md:col-span-2 flex justify-end">
                  <Button type="submit" variant="outline" className="text-sm">
                    Apply filters
                  </Button>
                </div>
              </form>
            </div>

            {/* Filter Pills */}
            <div className="flex flex-wrap gap-2" aria-label="Active queue filters">
              {[...new Set(active.keys())].map((key) => (
                <Button
                  key={key}
                  size="sm"
                  variant="outline"
                  aria-label={`Remove ${readable(key)} filter`}
                  onClick={() => {
                    const next = new URLSearchParams(filters);
                    next.delete(key);
                    setFilters(next.toString());
                  }}
                  className="text-sm gap-1.5 border-slate-200 bg-slate-50/70 text-slate-700 hover:bg-slate-100"
                >
                  <span>{readable(key)}: {active.getAll(key).map(readable).join(", ")}</span>
                  <span className="font-bold text-slate-400">×</span>
                </Button>
              ))}
            </div>

            {/* Live Counters */}
            <div
              className="flex flex-wrap gap-6 border-t border-slate-100 pt-3 text-sm font-medium text-slate-600"
              aria-live="polite"
            >
              <span>Pending: {counts?.pending ?? 0}</span>
              <span>Investigating: {counts?.investigating ?? 0}</span>
              <span>Closed: {counts?.closed ?? 0}</span>
              <span className="ml-auto text-slate-400">Priority first · newest first</span>
            </div>

            <Problem error={queue.error} retry={() => void queue.refetch()} />
            {queue.isFetching && !queue.data && (
              <div className="flex items-center justify-center py-12 text-sm text-slate-500 gap-2">
                <span className="inline-block size-4 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
                <p role="status">Loading cases…</p>
              </div>
            )}
            {queue.isSuccess && !queue.error && items.length === 0 && (
              <div className="py-12 text-center text-sm text-slate-500 italic">
                No cases match these filters.
              </div>
            )}

            {/* Queue Table */}
            {!queue.error && identity.isSuccess && items.length > 0 && (
              <div className="overflow-x-auto rounded-xl border border-slate-200/90 bg-white shadow-2xs">
                <table className="w-full text-left text-sm">
                  <thead className="border-b border-slate-200 bg-slate-50/80 text-slate-700 font-semibold uppercase tracking-wider text-sm">
                    <tr>
                      <th className="w-12 px-4 py-3.5">
                        <SelectPageCheckbox
                          checked={items.length > 0 && selectedCaseIds.length === items.length}
                          mixed={selectedCaseIds.length > 0 && selectedCaseIds.length < items.length}
                          disabled={items.length === 0}
                          onChange={toggleAll}
                          ariaLabel="Select all cases on this page"
                        />
                      </th>
                      {[
                        "Case",
                        "Inbox",
                        "Lifecycle / attention",
                        "Outcome",
                        "Assignee",
                        "Created",
                      ].map((name) => (
                        <th key={name} className="px-4 py-3.5 font-semibold">
                          {name}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {items.map((item) => {
                      const isSelected = selectedCaseIds.includes(item.id);
                      const isItemOverdue =
                        item.status !== "closed" &&
                        item.sla_due_at &&
                        new Date(item.sla_due_at) <= new Date();

                      return (
                        <tr
                          key={item.id}
                          className={cn(
                            "transition-colors text-sm text-slate-900",
                            isSelected ? "bg-blue-50/50 hover:bg-blue-50/70" : "hover:bg-slate-50/80"
                          )}
                        >
                          <td className="w-12 px-4 py-3.5">
                            <input
                              type="checkbox"
                              className="size-4 rounded border-slate-300 accent-blue-700 text-blue-600 focus:ring-blue-500 cursor-pointer"
                              aria-label={`Select case ${item.name}`}
                              value={item.id}
                              checked={isSelected}
                              onChange={() => toggleCase(item.id)}
                            />
                          </td>

                          {/* Case Name & Info */}
                          <td className="px-4 py-3.5">
                            <Link
                              className="font-semibold text-blue-700 hover:underline block max-w-sm truncate text-sm"
                              href={`/cases/${item.id}?tenant=${tenant}&queue=${encodeURIComponent(
                                filters
                              )}`}
                            >
                              {item.name}
                            </Link>
                            <div className="mt-1 flex flex-wrap items-center gap-2 text-sm text-slate-500">
                              <span className="font-mono text-slate-400">
                                {item.id.slice(0, 8)}
                              </span>
                              {item.tags && item.tags.length > 0 && (
                                <>
                                  <span>·</span>
                                  <span className="text-slate-600">
                                    {item.tags
                                      .map(
                                        (tag) =>
                                          tag.name + (tag.deleted_at ? " (archived)" : "")
                                      )
                                      .join(", ")}
                                  </span>
                                </>
                              )}
                            </div>
                          </td>

                          {/* Inbox */}
                          <td className="px-4 py-3.5 font-medium text-slate-700 text-sm">
                            {inboxes.data?.inboxes.find(
                              (inbox) => inbox.id === item.inbox_id
                            )?.name ?? item.inbox_id}
                          </td>

                          {/* Lifecycle / Attention */}
                          <td className="px-4 py-3.5 capitalize text-sm">
                            <div className="flex flex-col gap-1 items-start">
                              <StatusPill value={item.status} />
                              {item.snoozed_until &&
                                new Date(item.snoozed_until) > new Date() && (
                                  <div className="text-sm font-medium text-amber-700 flex items-center gap-1">
                                    <Clock className="size-3.5" />
                                    Snoozed until {timestamp(item.snoozed_until)}
                                  </div>
                                )}
                              {item.boost_reason && (
                                <div className="text-sm font-medium text-blue-700">
                                  Attention: {readable(item.boost_reason)}
                                </div>
                              )}
                            </div>
                          </td>

                          {/* Outcome */}
                          <td className="px-4 py-3.5 capitalize text-sm">
                            {item.outcome && item.outcome !== "unset" ? (
                              <StatusPill value={item.outcome} />
                            ) : (
                              <span className="text-slate-400 italic">
                                {readable(item.outcome)}
                              </span>
                            )}
                          </td>

                          {/* Assignee */}
                          <td className="px-4 py-3.5">
                            <div className="flex items-center gap-1.5 text-slate-700 font-medium truncate max-w-[150px] text-sm">
                              <User className="size-4 text-slate-400 shrink-0" />
                              <span>{item.assigned_to ?? "Unassigned"}</span>
                            </div>
                          </td>

                          {/* Created & SLA */}
                          <td className="px-4 py-3.5 whitespace-nowrap text-sm">
                            <div className="text-slate-700 font-medium">
                              {timestamp(item.created_at)}
                            </div>
                            {item.sla_due_at && (
                              <div
                                className={cn(
                                  "text-sm font-medium mt-0.5",
                                  isItemOverdue ? "text-red-700" : "text-slate-500"
                                )}
                              >
                                {isItemOverdue ? "Overdue: " : ""}SLA due {timestamp(item.sla_due_at)}
                              </div>
                            )}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}

            {queue.hasNextPage && (
              <Button
                variant="outline"
                disabled={queue.isFetchingNextPage}
                onClick={() => void queue.fetchNextPage()}
                className="text-sm mt-3"
              >
                {queue.isFetchingNextPage ? "Loading…" : "Load more cases"}
              </Button>
            )}
          </Panel>
        </div>

        {bulkModalOpen && (
          <BulkActionsModal
            tenant={tenant}
            session={session}
            cases={items}
            selectedCaseIds={selectedCaseIds}
            inboxes={inboxes.data?.inboxes ?? []}
            onClose={() => setBulkModalOpen(false)}
            onRemoveCaseId={(id) =>
              setSelectedCaseIds((prev) => prev.filter((i) => i !== id))
            }
            onClearSelection={() => setSelectedCaseIds([])}
          />
        )}
      </div>
    );
  }
