"use client";

import { useState } from "react";
import { Tag as TagIcon, Plus, Archive, Palette } from "lucide-react";
import { useCaseContext, useCaseWrite } from "@/lib/case-manager-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { CaseNav, Panel, Problem, labelClass, fieldClass } from "./shared";
import { cn } from "@/lib/utils";

export function TagSettings({
  tenant,
  session,
}: {
  tenant: string;
  session: string;
}) {
  const { identity, tags } = useCaseContext(tenant, session);
  const write = useCaseWrite(tenant, session);

  return (
    <div className="space-y-6">
      <CaseNav tenant={tenant} />

      <header className="rounded-2xl border border-slate-200 bg-white p-6 shadow-xs">
        <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-slate-900">
          Case tags
        </h1>
        <p className="mt-1 text-sm text-slate-500">
          Manage categorization tags, investigation markers, and color codes for cases.
        </p>
      </header>

      <Problem error={identity.error ?? tags.error ?? write.error} />

      {identity.isPending && (
        <div className="flex items-center gap-2 py-4 text-sm text-slate-500">
          <span className="inline-block size-3.5 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
          <p>Checking permissions…</p>
        </div>
      )}

      {identity.isSuccess && !identity.data.admin && (
        <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">
          Tag settings require a tenant administrator.
        </p>
      )}

      {identity.data?.admin && (
        <div className="space-y-6">
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
                  /* Displayed above. */
                }
              }}
            >
              <label className={cn(labelClass, "flex-1 min-w-[200px]")}>
                <span>Name</span>
                <Input name="name" required maxLength={200} placeholder="e.g. High Priority, VIP, Account Takeover" className="font-normal normal-case" />
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

              <Button type="submit" disabled={write.isPending} className="text-sm font-medium gap-1.5">
                <Plus className="size-3.5" />
                Create tag
              </Button>
            </form>
          </Panel>

          {/* Active Tags */}
          <Panel title="Active tags">
            <p className="text-sm text-slate-500">
              Archiving prevents new attachments. Existing cases retain the tag and its history.
            </p>

            {tags.isPending && <p className="text-sm text-slate-500">Loading tags…</p>}
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
                      /* Displayed above. */
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
                      className="font-normal normal-case"
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
                    <Button type="submit" disabled={write.isPending} className="text-sm">
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
  );
}
