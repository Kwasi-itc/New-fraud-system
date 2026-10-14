"use client";

import { useState } from "react";
import { Inbox, Tag as TagIcon, Sliders, Shield, Plus, Archive, User } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  useCaseContext,
  useCaseQuery,
  useCaseWrite,
} from "@/lib/case-manager-query";
import type { Inbox as InboxType, InboxUser } from "@/lib/case-manager-api";
import {
  CaseNav,
  Panel,
  Problem,
  fieldClass,
  labelClass,
} from "./shared";
import { cn } from "@/lib/utils";

export function CaseSettings({
  tenant,
  session,
  defaultTab = "inboxes",
}: {
  tenant: string;
  session: string;
  defaultTab?: "inboxes" | "tags";
}) {
  const [activeTab, setActiveTab] = useState<"inboxes" | "tags">(defaultTab);
  const { identity, inboxes, tags } = useCaseContext(tenant, session);
  const write = useCaseWrite(tenant, session);

  // Inboxes state
  const [selectedInboxId, setSelectedInboxId] = useState("");
  const users = useCaseQuery<{ users: InboxUser[] }>(
    tenant,
    session,
    `inboxes/${selectedInboxId}/users`,
    !!selectedInboxId && !!identity.data?.admin
  );
  const selectedInbox = inboxes.data?.inboxes.find(
    (item) => item.id === selectedInboxId
  );

  return (
    <div className="space-y-4">
      <CaseNav tenant={tenant} />

      {/* Settings Header with Tab Switcher */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-slate-200/90 pb-3">
        <div>
          <h1 className="text-xl sm:text-2xl font-bold tracking-tight text-slate-900">
            Settings
          </h1>
          <p className="mt-0.5 text-sm text-slate-500">
            Manage investigation inboxes, routing policies, and case taxonomy tags.
          </p>
        </div>

        {/* Minimalist Sub-tabs */}
        <div className="inline-flex items-center rounded-xl border border-slate-200 bg-slate-100/70 p-1">
          <button
            type="button"
            onClick={() => setActiveTab("inboxes")}
            className={cn(
              "flex items-center gap-2 rounded-lg px-3.5 py-1.5 text-sm font-semibold transition-all",
              activeTab === "inboxes"
                ? "bg-white text-slate-900 shadow-2xs"
                : "text-slate-600 hover:text-slate-900"
            )}
          >
            <Inbox className="size-4" />
            Inboxes
            {inboxes.data?.inboxes && (
              <span className="rounded-full bg-slate-200/80 px-2 py-0.2 text-sm font-bold text-slate-700">
                {inboxes.data.inboxes.length}
              </span>
            )}
          </button>
          <button
            type="button"
            onClick={() => setActiveTab("tags")}
            className={cn(
              "flex items-center gap-2 rounded-lg px-3.5 py-1.5 text-sm font-semibold transition-all",
              activeTab === "tags"
                ? "bg-white text-slate-900 shadow-2xs"
                : "text-slate-600 hover:text-slate-900"
            )}
          >
            <TagIcon className="size-4" />
            Tags
            {tags.data?.tags && (
              <span className="rounded-full bg-slate-200/80 px-2 py-0.2 text-sm font-bold text-slate-700">
                {tags.data.tags.length}
              </span>
            )}
          </button>
        </div>
      </div>

      <Problem
        error={identity.error ?? inboxes.error ?? tags.error ?? write.error}
        retry={() => {
          void identity.refetch();
          void inboxes.refetch();
          void tags.refetch();
        }}
      />

      {identity.isPending && (
        <div className="flex items-center gap-2 py-4 text-sm text-slate-500">
          <span className="inline-block size-3.5 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
          <p>Checking permissions…</p>
        </div>
      )}

      {identity.isSuccess && !identity.data.admin && (
        <p
          role="alert"
          className="rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800"
        >
          Settings require a tenant administrator.
        </p>
      )}

      {identity.data?.admin && (
        <div className="space-y-4">
          {/* TAB 1: INBOXES */}
          {activeTab === "inboxes" && (
            <div className="space-y-4">
              {/* Compact Inboxes Header & Actions Toolbar */}
              <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-xs">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div className="flex-1 min-w-[240px]">
                    <label className={labelClass}>
                      <span>Select Inbox to Configure</span>
                      <select
                        className={cn(fieldClass, "font-normal normal-case mt-1 text-sm")}
                        value={selectedInboxId}
                        onChange={(event) => setSelectedInboxId(event.target.value)}
                      >
                        <option value="">Select an inbox to configure</option>
                        {inboxes.data?.inboxes.map((item) => (
                          <option key={item.id} value={item.id}>
                            {item.name} ({item.status})
                          </option>
                        ))}
                      </select>
                    </label>
                  </div>

                  <div className="w-full sm:w-auto border-t sm:border-t-0 sm:border-l border-slate-200 pt-3 sm:pt-0 sm:pl-4">
                    <form
                      className="flex items-end gap-2"
                      onSubmit={async (event) => {
                        event.preventDefault();
                        const form = event.currentTarget;
                        try {
                          const result = (await write.mutateAsync({
                            path: "inboxes",
                            payload: { name: new FormData(form).get("name") },
                          })) as { inbox: InboxType };
                          setSelectedInboxId(result.inbox.id);
                          form.reset();
                        } catch {
                          /* Error displayed by Problem */
                        }
                      }}
                    >
                      <label className={labelClass}>
                        <span>New inbox</span>
                        <Input
                          name="name"
                          required
                          placeholder="e.g. Fraud Escalations"
                          className="font-normal normal-case text-sm h-9 min-w-[200px]"
                        />
                      </label>
                      <Button
                        type="submit"
                        disabled={write.isPending}
                        className="h-9 text-sm font-medium gap-1.5 shrink-0"
                      >
                        <Plus className="size-4" />
                        Create
                      </Button>
                    </form>
                  </div>
                </div>
              </div>

              {selectedInbox && (
                <div className="space-y-4">
                  {/* Routing & Availability */}
                  <Panel title="Routing and availability">
                    <form
                      key={`${selectedInbox.id}:${selectedInbox.status}:${selectedInbox.escalation_inbox_id}:${selectedInbox.sla_days}:${selectedInbox.auto_assign_enabled}`}
                      className="grid gap-4 md:grid-cols-2"
                      onSubmit={async (event) => {
                        event.preventDefault();
                        const data = new FormData(event.currentTarget);
                        try {
                          await write.mutateAsync({
                            path: `inboxes/${selectedInboxId}`,
                            method: "PATCH",
                            payload: {
                              name: data.get("name"),
                              status: data.get("status"),
                              escalation_inbox_id: data.get("destination") || null,
                              sla_days: data.get("sla_days")
                                ? Number(data.get("sla_days"))
                                : null,
                              auto_assign_enabled: data.get("auto_assign") === "on",
                            },
                          });
                        } catch {
                          /* Error displayed by Problem */
                        }
                      }}
                    >
                      <label className={labelClass}>
                        <span>Name</span>
                        <Input
                          name="name"
                          defaultValue={selectedInbox.name}
                          required
                          className="font-normal normal-case text-sm"
                        />
                      </label>

                      <label className={labelClass}>
                        <span>Status</span>
                        <select
                          name="status"
                          defaultValue={selectedInbox.status}
                          className={cn(fieldClass, "font-normal normal-case text-sm")}
                        >
                          <option value="active">Active</option>
                          <option value="archived">Archived</option>
                        </select>
                      </label>

                      <label className={labelClass}>
                        <span>Escalation destination</span>
                        <select
                          name="destination"
                          defaultValue={selectedInbox.escalation_inbox_id ?? ""}
                          className={cn(fieldClass, "font-normal normal-case text-sm")}
                        >
                          <option value="">No escalation destination</option>
                          {inboxes.data?.inboxes
                            .filter(
                              (item) =>
                                item.id !== selectedInbox.id && item.status === "active"
                            )
                            .map((item) => (
                              <option key={item.id} value={item.id}>
                                {item.name}
                              </option>
                            ))}
                        </select>
                      </label>

                      <label className={labelClass}>
                        <span>SLA days (blank disables)</span>
                        <Input
                          name="sla_days"
                          type="number"
                          min={1}
                          max={3650}
                          defaultValue={selectedInbox.sla_days ?? ""}
                          className="font-normal normal-case text-sm"
                        />
                      </label>

                      <div className="md:col-span-2 pt-1">
                        <label className="flex items-center gap-2 text-sm font-medium text-slate-700 cursor-pointer">
                          <input
                            name="auto_assign"
                            type="checkbox"
                            defaultChecked={selectedInbox.auto_assign_enabled}
                            className="rounded border-slate-300 text-blue-600 focus:ring-blue-500"
                          />
                          Automatically assign eligible members
                        </label>
                      </div>

                      <p className="text-sm text-slate-500 md:col-span-2">
                        Archived inboxes retain history. New cases, inbound moves, and reopening
                        require an active inbox. Automatic assignment runs periodically.
                      </p>

                      <div className="md:col-span-2">
                        <Button
                          type="submit"
                          disabled={write.isPending}
                          className="text-sm font-medium"
                        >
                          Save inbox
                        </Button>
                      </div>
                    </form>
                  </Panel>

                  {/* Inbox Membership */}
                  <Panel title="Inbox membership">
                    <Problem error={users.error} retry={() => void users.refetch()} />
                    {users.isPending && (
                      <p className="text-sm text-slate-500">Loading members…</p>
                    )}

                    <ul className="divide-y divide-slate-100">
                      {users.data?.users.map((user) => (
                        <li
                          key={user.user_id}
                          className="flex flex-wrap items-center justify-between gap-3 py-3 text-sm"
                        >
                          <div className="flex items-center gap-2.5">
                            <User className="size-4 text-slate-400" />
                            <div>
                              <span className="font-semibold text-slate-900">
                                {user.user_id}
                              </span>
                              <div className="flex items-center gap-2 mt-0.5 text-slate-500 text-sm">
                                <span>
                                  Capacity:{" "}
                                  <strong className="text-slate-800">{user.capacity}</strong>
                                </span>
                                <span>•</span>
                                <span>
                                  Auto assign:{" "}
                                  <strong className="text-slate-800">
                                    {user.auto_assign_enabled ? "on" : "off"}
                                  </strong>
                                </span>
                              </div>
                            </div>
                          </div>

                          <Button
                            variant="outline"
                            size="sm"
                            disabled={write.isPending}
                            onClick={() =>
                              void write
                                .mutateAsync({
                                  path: `inboxes/${selectedInboxId}/users/${encodeURIComponent(
                                    user.user_id
                                  )}`,
                                  method: "DELETE",
                              })
                              .catch(() => {})
                            }
                            className="text-sm text-rose-700 border-rose-200 hover:bg-rose-50"
                          >
                            Remove membership
                          </Button>
                        </li>
                      ))}
                    </ul>

                    {users.isSuccess && users.data.users.length === 0 && (
                      <p className="text-sm text-slate-500 italic py-2">
                        No members assigned to this inbox yet.
                      </p>
                    )}

                    <div className="mt-4 pt-4 border-t border-slate-100">
                      <h4 className="text-sm font-semibold uppercase tracking-wider text-slate-600 mb-3">
                        Add or Update Member
                      </h4>
                      <form
                        className="grid gap-3 sm:grid-cols-3 items-end"
                        onSubmit={async (event) => {
                          event.preventDefault();
                          const form = event.currentTarget;
                          const user = String(new FormData(form).get("user")).trim();
                          try {
                            await write.mutateAsync({
                              path: `inboxes/${selectedInboxId}/users/${encodeURIComponent(
                                user
                              )}`,
                              method: "PUT",
                              payload: {
                                auto_assign_enabled:
                                  new FormData(form).get("auto_assign") === "on",
                                capacity: Number(new FormData(form).get("capacity")),
                              },
                            });
                            form.reset();
                          } catch {
                            /* Shown above */
                          }
                        }}
                      >
                        <label className={labelClass}>
                          <span>Identity provider subject</span>
                          <Input
                            name="user"
                            required
                            placeholder="User subject ID"
                            className="font-normal normal-case text-sm"
                          />
                        </label>

                        <label className={labelClass}>
                          <span>Open case capacity</span>
                          <Input
                            name="capacity"
                            type="number"
                            min={0}
                            max={1000}
                            defaultValue={20}
                            required
                            className="font-normal normal-case text-sm"
                          />
                        </label>

                        <div className="flex flex-col gap-2">
                          <label className="flex items-center gap-2 text-sm font-medium text-slate-700 cursor-pointer">
                            <input
                              name="auto_assign"
                              type="checkbox"
                              className="rounded border-slate-300 text-blue-600 focus:ring-blue-500"
                            />
                            Eligible for automatic assignment
                          </label>
                          <Button
                            type="submit"
                            disabled={write.isPending}
                            className="text-sm"
                          >
                            Add / update member
                          </Button>
                        </div>
                      </form>
                    </div>
                  </Panel>
                </div>
              )}
            </div>
          )}

          {/* TAB 2: TAGS */}
          {activeTab === "tags" && (
            <div className="space-y-4">
              {/* Create Tag Form */}
              <Panel title="Create tag">
                <form
                  className="flex flex-wrap items-end gap-3"
                  onSubmit={async (event) => {
                    event.preventDefault();
                    const form = event.currentTarget;
                    const data = new FormData(form);
                    try {
                      await write.mutateAsync({
                        path: "tags",
                        payload: {
                          name: data.get("name"),
                          color: data.get("color"),
                          target: "case",
                        },
                      });
                      form.reset();
                    } catch {
                      /* Displayed by Problem */
                    }
                  }}
                >
                  <label className={cn(labelClass, "flex-1 min-w-[200px]")}>
                    <span>Name</span>
                    <Input
                      name="name"
                      required
                      maxLength={200}
                      placeholder="e.g. High Priority, VIP, Account Takeover"
                      className="font-normal normal-case text-sm"
                    />
                  </label>

                  <label className={labelClass}>
                    <span>Color</span>
                    <div className="flex items-center gap-2">
                      <Input
                        name="color"
                        type="color"
                        defaultValue="#2563eb"
                        className="h-9 w-14 p-1 cursor-pointer"
                      />
                    </div>
                  </label>

                  <Button
                    type="submit"
                    disabled={write.isPending}
                    className="text-sm font-medium gap-1.5"
                  >
                    <Plus className="size-4" />
                    Create tag
                  </Button>
                </form>
              </Panel>

              {/* Active Tags */}
              <Panel title="Active tags">
                <p className="text-sm text-slate-500">
                  Archiving prevents new attachments. Existing cases retain the tag and its history.
                </p>

                {tags.isPending && (
                  <p className="text-sm text-slate-500">Loading tags…</p>
                )}
                {tags.data?.tags.length === 0 && (
                  <p className="text-sm text-slate-500 italic py-2">No active tags.</p>
                )}

                <div className="divide-y divide-slate-100">
                  {tags.data?.tags.map((tag) => (
                    <form
                      key={`${tag.id}:${tag.name}:${tag.color}`}
                      className="flex flex-wrap items-end gap-3 py-3"
                      onSubmit={async (event) => {
                        event.preventDefault();
                        const data = new FormData(event.currentTarget);
                        try {
                          await write.mutateAsync({
                            path: `tags/${tag.id}`,
                            method: "PATCH",
                            payload: {
                              name: data.get("name"),
                              color: data.get("color"),
                            },
                          });
                        } catch {
                          /* Displayed by Problem */
                        }
                      }}
                    >
                      <label className={cn(labelClass, "flex-1 min-w-[180px]")}>
                        <span>Name</span>
                        <Input
                          aria-label={`Name for ${tag.name}`}
                          name="name"
                          required
                          maxLength={200}
                          defaultValue={tag.name}
                          className="font-normal normal-case text-sm"
                        />
                      </label>

                      <label className={cn(labelClass, "w-28")}>
                        <span>Color</span>
                        <Input
                          name="color"
                          maxLength={64}
                          defaultValue={tag.color}
                          className="font-normal normal-case font-mono text-sm"
                        />
                      </label>

                      <div className="flex gap-2">
                        <Button
                          type="submit"
                          disabled={write.isPending}
                          className="text-sm"
                        >
                          Save tag
                        </Button>
                        <Button
                          type="button"
                          variant="outline"
                          disabled={write.isPending}
                          onClick={() =>
                            void write
                              .mutateAsync({
                                path: `tags/${tag.id}`,
                                method: "DELETE",
                              })
                              .catch(() => {})
                          }
                          className="text-sm text-rose-700 border-rose-200 hover:bg-rose-50"
                        >
                          Archive {tag.name}
                        </Button>
                      </div>
                    </form>
                  ))}
                </div>
              </Panel>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
