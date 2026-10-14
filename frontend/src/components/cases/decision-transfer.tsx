"use client";

import { useId, useRef, useState } from "react";
import { ArrowRightLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useCaseQueue, useCaseWrite } from "@/lib/case-manager-query";
import { fieldClass, Problem } from "./shared";
import { cn } from "@/lib/utils";

export function DecisionTransfer({
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
  const label = useId();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [target, setTarget] = useState("");
  const [reason, setReason] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [validation, setValidation] = useState("");
  const operation = useRef<{ signature: string; id: string } | null>(null);
  const queue = useCaseQueue(
    tenant,
    session,
    `status=pending&status=investigating&include_snoozed=true&name=${encodeURIComponent(
      search
    )}`,
    open
  );
  const write = useCaseWrite(tenant, session);
  const options = queue.error
    ? []
    : queue.data?.pages
        .flatMap((page) => page.cases)
        .filter((item) => item.id !== caseId && item.type === "decision") ?? [];
  const destination = options.find((item) => item.id === target);

  if (!open)
    return (
      <Button
        size="sm"
        variant="outline"
        onClick={() => setOpen(true)}
        className="gap-1.5 text-sm font-medium border-slate-200 hover:bg-slate-50 transition-colors"
      >
        <ArrowRightLeft className="size-3.5 text-slate-500" />
        Transfer decision
      </Button>
    );

  return (
    <form
      className="space-y-3 rounded-xl border border-slate-200 bg-white p-4 shadow-xs"
      onSubmit={async (event) => {
        event.preventDefault();
        if (
          !reason.trim() ||
          new TextEncoder().encode(reason.trim()).length > 10000
        ) {
          setValidation("Enter a transfer reason of at most 10000 bytes.");
          return;
        }
        if (!destination) {
          setValidation("Choose an accessible open decision case.");
          return;
        }
        setValidation("");
        if (!confirm) {
          setConfirm(true);
          return;
        }
        const signature = JSON.stringify([caseId, decisionId, target, reason.trim()]);
        if (operation.current?.signature !== signature)
          operation.current = { signature, id: crypto.randomUUID() };
        try {
          await write.mutateAsync({
            path: `cases/${caseId}/decision-transfers`,
            payload: {
              decision_id: decisionId,
              to_case_id: target,
              reason: reason.trim(),
              operation_id: operation.current.id,
            },
          });
        } catch {
          /* Preserve fields and operation ID for a safe retry. */
        }
      }}
    >
      <fieldset disabled={write.isPending} className="space-y-3">
        <p className="text-sm text-slate-600">
          Move this decision and its linked screening evidence. Both cases retain transfer history; their outcomes remain unchanged.
        </p>

        <label
          className="block text-sm font-semibold uppercase tracking-wider text-slate-600"
          htmlFor={`${label}-search`}
        >
          Find destination case
        </label>
        <Input
          id={`${label}-search`}
          value={search}
          placeholder="Filter open destination cases..."
          onChange={(event) => {
            setSearch(event.target.value);
            setTarget("");
            setConfirm(false);
          }}
          className="font-normal normal-case"
        />

        <Problem error={queue.error} retry={() => void queue.refetch()} />
        {queue.isPending && (
          <p role="status" className="text-sm text-slate-500">
            Loading destination cases…
          </p>
        )}

        <label
          className="block text-sm font-semibold uppercase tracking-wider text-slate-600"
          htmlFor={`${label}-target`}
        >
          Destination case
        </label>
        <select
          id={`${label}-target`}
          className={cn(fieldClass, "font-normal normal-case")}
          required
          value={target}
          onChange={(event) => {
            setTarget(event.target.value);
            setConfirm(false);
          }}
        >
          <option value="">Choose a case</option>
          {options.map((item) => (
            <option key={item.id} value={item.id}>
              {item.name} · {item.id}
            </option>
          ))}
        </select>

        {queue.isSuccess && !options.length && (
          <p className="text-sm text-slate-500 italic">No accessible open decision cases match.</p>
        )}
        {queue.hasNextPage && (
          <Button
            type="button"
            variant="outline"
            disabled={queue.isFetchingNextPage}
            onClick={() => void queue.fetchNextPage()}
            className="text-sm"
          >
            More destination cases
          </Button>
        )}

        <label
          className="block text-sm font-semibold uppercase tracking-wider text-slate-600"
          htmlFor={`${label}-reason`}
        >
          Transfer reason
        </label>
        <textarea
          id={`${label}-reason`}
          className={cn(fieldClass, "font-normal normal-case")}
          required
          rows={3}
          placeholder="State reason for transferring this decision to another case..."
          value={reason}
          onChange={(event) => {
            setReason(event.target.value);
            setConfirm(false);
          }}
        />

        {validation && (
          <p role="alert" className="text-sm font-medium text-rose-700">
            {validation}
          </p>
        )}
        <Problem error={write.error} />

        {confirm && destination && (
          <p role="status" className="rounded-lg bg-blue-50 p-2.5 text-sm font-medium text-blue-900 border border-blue-200">
            Confirm transfer to {destination.name}. This removes the decision from this case.
          </p>
        )}

        <div className="flex gap-2 pt-1">
          <Button
            type="submit"
            disabled={!destination || write.isPending}
            className="text-sm font-medium"
          >
            {write.isPending
              ? "Transferring…"
              : confirm
              ? "Confirm transfer"
              : "Review transfer"}
          </Button>
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              setOpen(false);
              setConfirm(false);
            }}
            className="text-sm"
          >
            Cancel
          </Button>
        </div>
      </fieldset>
    </form>
  );
}
