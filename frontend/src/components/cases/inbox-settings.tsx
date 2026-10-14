"use client";

import { useState } from "react";
import {
  Archive,
  CheckCircle2,
  Inbox,
  Plus,
  Shield,
  Trash2,
  User,
  Users,
} from "lucide-react";
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
import { StatusPill } from "./structured-data-viewer";
import { cn } from "@/lib/utils";

export function InboxSettings({
  tenant,
  session,
}: {
  tenant: string;
  session: string;
}) {
  const { identity, inboxes } = useCaseContext(tenant, session);
  const write = useCaseWrite(tenant, session);
  const [selected, setSelected] = useState("");
  const users = useCaseQuery<{ users: InboxUser[] }>(
    tenant,
    session,
    `inboxes/${selected}/users`,
    !!selected && !!identity.data?.admin
  );
  const inbox = inboxes.data?.inboxes.find((item) => item.id === selected);

  if (identity.error)
    return <Problem error={identity.error} retry={() => void identity.refetch()} />;

  return (
    <div className="space-y-6">
      <CaseNav tenant={tenant} />

      <header className="rounded-2xl border border-slate-200 bg-white p-6 shadow-xs">
        <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-slate-900">
          Inbox settings
        </h1>
        <p className="mt-1 text-sm text-slate-500">
          Configure triage queues, escalation routing, and investigator membership.
        </p>
      </header>

      {identity.isPending && (
        <div className="flex items-center gap-2 py-4 text-sm text-slate-500">
          <span className="inline-block size-3.5 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
          <p>Checking permissions…</p>
        </div>
      )}

      {identity.isSuccess && !identity.data.admin && (
        <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">
          Inbox settings require a tenant administrator.
        </p>
      )}

      {identity.data?.admin && (
        <div className="space-y-6">
          <Problem error={write.error ?? inboxes.error} />

          {/* Create Inbox Form */}
          <Panel title="Create inbox">
            <form
              className="flex flex-wrap items-end gap-3"
              onSubmit={async (event) => {
                event.preventDefault();
                const form = event.currentTarget;
                try {
                  const result = (await write.mutateAsync({
                    path: "inboxes",
                    payload: { name: new FormData(form).get("name") },
                  })) as { inbox: InboxType };
                  setSelected(result.inbox.id);
                  form.reset();
                } catch {
                  /* Shown above. */
                }
              }}
            >
              <label className={cn(labelClass, "flex-1 min-w-[240px]")}>
                <span>Name</span>
                <Input name="name" required placeholder="e.g. AML Investigations, Fraud Escalations" className="font-normal normal-case" />
              </label>
              <Button type="submit" disabled={write.isPending} className="text-sm font-medium">
                Create inbox
              </Button>
            </form>
          </Panel>

          {/* Select Inbox for Configuration */}
          <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-xs">
            <label className={labelClass}>
              <span>Select Inbox to Configure</span>
              <select
                className={cn(fieldClass, "font-normal normal-case mt-1.5")}
                value={selected}
                onChange={(event) => setSelected(event.target.value)}
              >
                <option value="">Select an inbox</option>
                {inboxes.data?.inboxes.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name} ({item.status})
                  </option>
                ))}
              </select>
            </label>
          </div>

          {inbox && (
            <div className="space-y-6">
              {/* Routing & Availability */}
              <Panel title="Routing and availability">
                <form
                  key={`${inbox.id}:${inbox.status}:${inbox.escalation_inbox_id}:${inbox.sla_days}:${inbox.auto_assign_enabled}`}
                  className="grid gap-4 md:grid-cols-2"
                  onSubmit={async (event) => {
                    event.preventDefault();
                    const data = new FormData(event.currentTarget);
                    try {
                      await write.mutateAsync({
                        path: `inboxes/${selected}`,
                        method: "PATCH",
                        payload: {
                          name: data.get("name"),
                          status: data.get("status"),
                          escalation_inbox_id: data.get("destination") || null,
                          sla_days: data.get("sla_days") ? Number(data.get("sla_days")) : null,
                          auto_assign_enabled: data.get("auto_assign") === "on",
                        },
                      });
                    } catch {
                      /* Shown above. */
                    }
                  }}
                >
                  <label className={labelClass}>
                    <span>Name</span>
                    <Input name="name" defaultValue={inbox.name} required className="font-normal normal-case" />
                  </label>

                  <label className={labelClass}>
                    <span>Status</span>
                    <select
                      name="status"
                      defaultValue={inbox.status}
                      className={cn(fieldClass, "font-normal normal-case")}
                    >
                      <option value="active">Active</option>
                      <option value="archived">Archived</option>
                    </select>
                  </label>

                  <label className={labelClass}>
                    <span>Escalation destination</span>
                    <select
                      name="destination"
                      defaultValue={inbox.escalation_inbox_id ?? ""}
                      className={cn(fieldClass, "font-normal normal-case")}
                    >
                      <option value="">No escalation destination</option>
                      {inboxes.data?.inboxes
                        .filter((item) => item.id !== inbox.id && item.status === "active")
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
                      defaultValue={inbox.sla_days ?? ""}
                      className="font-normal normal-case"
                    />
                  </label>

                  <div className="md:col-span-2 pt-1">
                    <label className="flex items-center gap-2 text-sm font-medium text-slate-700 cursor-pointer">
                      <input
                        name="auto_assign"
                        type="checkbox"
                        defaultChecked={inbox.auto_assign_enabled}
                        className="rounded border-slate-300 text-blue-600 focus:ring-blue-500"
                      />
                      Automatically assign eligible members
                    </label>
                  </div>

                  <p className="text-sm text-slate-500 md:col-span-2">
                    Archived inboxes retain history. New cases, inbound moves, and reopening require an active inbox. Automatic assignment runs periodically. Automated reviews are not available yet.
                  </p>

                  <div className="md:col-span-2">
                    <Button type="submit" disabled={write.isPending} className="text-sm font-medium">
                      Save inbox
                    </Button>
                  </div>
                </form>
              </Panel>

              {/* Inbox Membership */}
              <Panel title="Inbox membership">
                <Problem error={users.error} retry={() => void users.refetch()} />
                {users.isPending && <p className="text-sm text-slate-500">Loading members…</p>}

                <ul className="divide-y divide-slate-100">
                  {users.data?.users.map((user) => (
                    <li
                      key={user.user_id}
                      className="flex flex-wrap items-center justify-between gap-3 py-3 text-sm"
                    >
                      <div className="flex items-center gap-2.5">
                        <User className="size-4 text-slate-400" />
                        <div>
                          <span className="font-semibold text-slate-900">{user.user_id}</span>
                          <div className="flex items-center gap-2 mt-0.5 text-slate-500 text-sm">
                            <span>Capacity: <strong className="text-slate-800">{user.capacity}</strong></span>
                            <span>•</span>
                            <span>Auto assign: <strong className="text-slate-800">{user.auto_assign_enabled ? "on" : "off"}</strong></span>
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
                              path: `inboxes/${selected}/users/${encodeURIComponent(user.user_id)}`,
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
                          path: `inboxes/${selected}/users/${encodeURIComponent(user)}`,
                          method: "PUT",
                          payload: {
                            auto_assign_enabled: new FormData(form).get("auto_assign") === "on",
                            capacity: Number(new FormData(form).get("capacity")),
                          },
                        });
                        form.reset();
                      } catch {
                        /* Shown above. */
                      }
                    }}
                  >
                    <label className={labelClass}>
                      <span>Identity provider subject</span>
                      <Input name="user" required placeholder="User subject ID" className="font-normal normal-case" />
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
                        className="font-normal normal-case"
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
                      <Button type="submit" disabled={write.isPending} className="text-sm">
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
    </div>
  );
}
