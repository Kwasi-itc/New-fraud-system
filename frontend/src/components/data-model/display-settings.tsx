"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Sliders,
  ArrowUp,
  ArrowDown,
  X,
  AlertCircle,
} from "lucide-react";
import { dataModelApi, type DisplayColumn, type DisplaySettings } from "@/lib/data-model-api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export function DisplaySettingsButton({
  tableId,
  tenant,
  name,
}: {
  tableId: string;
  tenant: string;
  name: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button
        variant="outline"
        onClick={() => setOpen(true)}
        aria-label={`Display settings for ${name}`}
        className="flex items-center gap-1.5 border-slate-300 text-slate-700 hover:bg-slate-50 hover:text-slate-900 shadow-sm"
      >
        <Sliders className="size-3.5 text-slate-500" />
        <span>Display settings</span>
      </Button>
      {open && <DisplaySettingsModal tableId={tableId} tenant={tenant} onClose={() => setOpen(false)} />}
    </>
  );
}

function DisplaySettingsModal({
  tableId,
  tenant,
  onClose,
}: {
  tableId: string;
  tenant: string;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [busy, setBusy] = useState(false);
  const query = useQuery({
    queryKey: ["data-model", "display-settings", tenant, tableId],
    queryFn: () => dataModelApi.getDisplaySettings(tableId),
    staleTime: 0,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
  });

  useEffect(() => {
    const element = dialog.current;
    const previous = document.activeElement as HTMLElement | null;
    element?.showModal();
    return () => {
      element?.close();
      previous?.focus();
    };
  }, []);

  return (
    <dialog
      ref={dialog}
      aria-labelledby="display-title"
      className="m-auto max-h-[92vh] w-[min(52rem,calc(100vw-2rem))] overflow-y-auto rounded-xl border border-slate-200 bg-white p-6 shadow-xl backdrop:bg-slate-950/38 text-slate-900"
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) onClose();
      }}
    >
      <div className="flex items-start justify-between gap-4 border-b border-slate-200 pb-4">
        <div>
          <h2 id="display-title" className="text-xl font-semibold tracking-tight text-slate-950">
            Display settings
          </h2>
          <p className="mt-1 text-sm text-slate-500">
            Configure default caption and column attributes displayed for this table across queues and investigation views.
          </p>
        </div>
        <button
          type="button"
          onClick={onClose}
          disabled={busy}
          className="rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-900 transition-colors disabled:opacity-50"
          aria-label="Close modal"
        >
          <X className="size-4" />
        </button>
      </div>

      {query.isPending && (
        <div className="flex flex-col items-center justify-center py-12 text-slate-500 gap-2">
          <div className="size-5 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
          <p className="text-xs">Loading display configuration…</p>
        </div>
      )}

      {query.isError && (
        <div role="alert" className="my-4 rounded-lg border border-red-200 bg-red-50 p-3 text-red-800 flex items-start gap-2.5 text-xs">
          <AlertCircle className="size-4 shrink-0 text-red-600 mt-0.5" />
          <div>
            <p className="font-semibold">Unable to load display settings</p>
            <p className="mt-0.5 text-red-700">{query.error.message}</p>
            <button
              className="mt-1.5 font-semibold underline hover:text-red-900"
              onClick={() => void query.refetch()}
            >
              Try again
            </button>
          </div>
        </div>
      )}

      {query.data && (
        <DisplayForm
          key={query.dataUpdatedAt}
          initial={query.data}
          tenant={tenant}
          onClose={onClose}
          reload={() => void query.refetch()}
          busy={busy}
          setBusy={setBusy}
        />
      )}

      {!query.data && !query.isPending && (
        <div className="flex justify-end pt-4">
          <Button variant="outline" onClick={onClose}>
            Close
          </Button>
        </div>
      )}
    </dialog>
  );
}

function DisplayForm({
  initial,
  tenant,
  onClose,
  reload,
  busy,
  setBusy,
}: {
  initial: DisplaySettings;
  tenant: string;
  onClose: () => void;
  reload: () => void;
  busy: boolean;
  setBusy: (busy: boolean) => void;
}) {
  const client = useQueryClient();

  const [captionField, setCaptionField] = useState(initial.caption_field || "");
  const [columns, setColumns] = useState<DisplayColumn[]>(() => {
    if (initial.display_columns && initial.display_columns.length > 0) {
      return initial.display_columns;
    }
    // Convert legacy displayed_fields if display_columns not yet stored
    const legacy: DisplayColumn[] = (initial.displayed_fields ?? []).map((id, idx) => {
      const choice = initial.field_display?.[id];
      const field = initial.fields.find((f) => f.id === id);
      if (choice?.link_id) {
        const link = initial.links.find((l) => l.id === choice.link_id);
        const targetField =
          link?.target_fields?.find((tf) => tf.name === link.caption_field) ?? link?.target_fields?.[0];
        return {
          id: `col_legacy_${id}_${idx}`,
          type: "relation",
          label: choice.label || (targetField ? targetField.alias || targetField.name : (field ? field.alias || field.name : "")),
          field_id: id,
          link_id: choice.link_id,
          target_field_id: targetField?.id,
        };
      }
      return {
        id: `col_legacy_${id}_${idx}`,
        type: "direct",
        label: choice?.label || (field ? field.alias || field.name : ""),
        field_id: id,
      };
    });
    return legacy;
  });

  const [addSelection, setAddSelection] = useState("");
  const [error, setError] = useState("");
  const submitting = useRef(false);

  function move(index: number, offset: number) {
    setColumns((old) => {
      const target = index + offset;
      if (target < 0 || target >= old.length) return old;
      const copy = [...old];
      [copy[index], copy[target]] = [copy[target], copy[index]];
      return copy;
    });
  }

  function remove(index: number) {
    setColumns((old) => old.filter((_, i) => i !== index));
  }

  function handleAddColumn(e: React.ChangeEvent<HTMLSelectElement>) {
    const val = e.target.value;
    if (!val) return;
    setAddSelection("");
    if (columns.length >= 12) {
      setError("At most 12 display columns are allowed.");
      return;
    }

    if (val.startsWith("direct:")) {
      const fieldId = val.slice("direct:".length);
      const field = initial.fields.find((f) => f.id === fieldId);
      if (!field) return;
      const newCol: DisplayColumn = {
        id: `col_${crypto.randomUUID()}`,
        type: "direct",
        label: field.alias || field.name,
        field_id: field.id,
      };
      setColumns((old) => [...old, newCol]);
    } else if (val.startsWith("relation:")) {
      // Format: relation:<link_id>:<target_field_id>
      const parts = val.slice("relation:".length).split(":");
      const linkId = parts[0];
      const targetFieldId = parts[1];
      const link = initial.links.find((l) => l.id === linkId);
      const targetField = link?.target_fields?.find((tf) => tf.id === targetFieldId);
      if (!link || !targetField) return;

      const newCol: DisplayColumn = {
        id: `col_${crypto.randomUUID()}`,
        type: "relation",
        label: targetField.alias || `${link.target_table}.${targetField.name}`,
        link_id: link.id,
        target_field_id: targetField.id,
      };
      setColumns((old) => [...old, newCol]);
    }
  }

  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (submitting.current) return;
    submitting.current = true;
    setBusy(true);
    setError("");

    try {
      // Construct backward-compatible legacy fields
      const displayedFields: string[] = [];
      const fieldDisplay: Record<string, { label?: string; link_id?: string }> = {};

      for (const col of columns) {
        if (col.type === "direct" && col.field_id) {
          if (!displayedFields.includes(col.field_id)) {
            displayedFields.push(col.field_id);
          }
          fieldDisplay[col.field_id] = { label: col.label };
        } else if (col.type === "relation" && col.link_id) {
          const link = initial.links.find((l) => l.id === col.link_id);
          if (link && !displayedFields.includes(link.field_id)) {
            displayedFields.push(link.field_id);
            fieldDisplay[link.field_id] = { label: col.label, link_id: link.id };
          }
        }
      }

      await dataModelApi.saveDisplaySettings(initial.table_id, {
        ...initial,
        caption_field: captionField,
        display_columns: columns,
        displayed_fields: displayedFields,
        field_order: displayedFields,
        field_display: fieldDisplay,
      });

      await client.invalidateQueries({ queryKey: ["data-model"] });
      await client.invalidateQueries({ queryKey: ["record-displays", tenant] });
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Display settings could not be saved.");
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  }

  return (
    <form onSubmit={save} className="mt-5 space-y-5">
      {error && (
        <div role="alert" className="rounded-lg border border-red-200 bg-red-50 p-3 text-xs text-red-800 flex items-start gap-2">
          <AlertCircle className="size-4 text-red-600 shrink-0 mt-0.5" />
          <div className="flex-1">
            <span className="font-semibold">{error}</span>
            <button type="button" className="ml-2 font-medium underline hover:text-red-950" onClick={reload}>
              Reload saved settings
            </button>
          </div>
        </div>
      )}

      <fieldset disabled={busy} className="space-y-5">
        {/* PRIMARY RECORD CAPTION */}
        <div className="space-y-1.5">
          <label className="block text-sm font-medium text-slate-900" htmlFor="display-caption-field">
            Primary record caption field
          </label>
          <select
            id="display-caption-field"
            className="block w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-900 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
            value={captionField}
            onChange={(e) => setCaptionField(e.target.value)}
          >
            <option value="">Use Object ID (default)</option>
            {initial.fields
              .filter((f) => f.data_type === "string")
              .map((f) => (
                <option key={f.id} value={f.name}>
                  {f.alias ? `${f.alias} (${f.name})` : f.name}
                </option>
              ))}
          </select>
          <p className="text-xs text-slate-500">
            Field used as the primary title in case queues, triggers, and entity views.
          </p>
        </div>

        {/* DISPLAY COLUMNS */}
        <div className="space-y-2.5 pt-1">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
            <div>
              <h3 className="text-sm font-medium text-slate-900">
                Display columns ({columns.length}/12)
              </h3>
              <p className="text-xs text-slate-500">
                Attributes displayed in tables and detail cards in the order specified below.
              </p>
            </div>

            {columns.length < 12 ? (
              <div>
                <select
                  aria-label="Add display column"
                  className="rounded-md border border-slate-200 bg-white px-2.5 py-1.5 text-xs font-medium text-slate-800 hover:bg-slate-50 cursor-pointer focus:border-[#2563eb]"
                  value={addSelection}
                  onChange={handleAddColumn}
                >
                  <option value="">+ Add column…</option>
                  <optgroup label="Direct fields">
                    {initial.fields.map((f) => (
                      <option key={f.id} value={`direct:${f.id}`}>
                        {f.alias ? `${f.alias} (${f.name})` : f.name}
                      </option>
                    ))}
                  </optgroup>
                  {initial.links.map((link) => (
                    <optgroup key={link.id} label={`Via ${link.name} → ${link.target_table}`}>
                      {(link.target_fields ?? []).map((tf) => (
                        <option key={tf.id} value={`relation:${link.id}:${tf.id}`}>
                          {link.target_table}.{tf.alias ? `${tf.alias} (${tf.name})` : tf.name}
                        </option>
                      ))}
                      {(!link.target_fields || link.target_fields.length === 0) && link.caption_field && (
                        <option value={`relation:${link.id}:caption`}>
                          {link.target_table}.{link.caption_field}
                        </option>
                      )}
                    </optgroup>
                  ))}
                </select>
              </div>
            ) : (
              <span className="text-xs text-slate-400">Maximum 12 columns reached</span>
            )}
          </div>

          {columns.length === 0 ? (
            <div className="rounded-lg border border-dashed border-slate-200 p-6 text-center text-xs text-slate-500">
              No display columns configured yet. Use &ldquo;+ Add column…&rdquo; to add fields from this table or linked relations.
            </div>
          ) : (
            <div className="overflow-x-auto rounded-lg border border-slate-200">
              <table className="w-full text-left text-xs">
                <thead className="border-b border-slate-200 bg-slate-50 font-medium text-slate-600">
                  <tr>
                    <th className="px-3 py-2">Field / Source</th>
                    <th className="px-3 py-2">Column label</th>
                    <th className="px-3 py-2 text-center w-20">Order</th>
                    <th className="px-3 py-2 text-right w-16"></th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {columns.map((col, index) => {
                    let sourceDescription = "";
                    let isRelation = false;
                    let relationPath = "";

                    if (col.type === "direct") {
                      const f = initial.fields.find((field) => field.id === col.field_id);
                      sourceDescription = f ? (f.alias ? `${f.alias} (${f.name})` : f.name) : "Direct field";
                    } else if (col.type === "relation") {
                      isRelation = true;
                      const link = initial.links.find((l) => l.id === col.link_id);
                      const tf = link?.target_fields?.find((field) => field.id === col.target_field_id);
                      relationPath = link ? `${link.name} → ${link.target_table}` : "";
                      sourceDescription = link && tf ? `${link.target_table}.${tf.alias ? `${tf.alias} (${tf.name})` : tf.name}` : "Related field";
                    }

                    return (
                      <tr key={col.id} className="hover:bg-slate-50/50">
                        <td className="px-3 py-2">
                          <div className="flex flex-col">
                            <span className="font-medium text-slate-900">
                              {sourceDescription}
                            </span>
                            {isRelation && (
                              <span className="text-[10px] text-slate-400">
                                via {relationPath}
                              </span>
                            )}
                          </div>
                        </td>
                        <td className="px-3 py-2">
                          <Input
                            aria-label={`Label for ${sourceDescription}`}
                            maxLength={100}
                            placeholder="Column label"
                            className="h-8 text-xs bg-white rounded border-slate-200"
                            value={col.label}
                            onChange={(e) => {
                              const newLabel = e.target.value;
                              setColumns((old) =>
                                old.map((c, i) => (i === index ? { ...c, label: newLabel } : c))
                              );
                            }}
                          />
                        </td>
                        <td className="px-3 py-2 text-center whitespace-nowrap">
                          <div className="inline-flex items-center gap-1">
                            <button
                              type="button"
                              aria-label="Move column up"
                              disabled={index === 0}
                              className="size-6 rounded flex items-center justify-center text-slate-400 hover:text-slate-800 disabled:opacity-25"
                              onClick={() => move(index, -1)}
                            >
                              <ArrowUp className="size-3.5" />
                            </button>
                            <button
                              type="button"
                              aria-label="Move column down"
                              disabled={index === columns.length - 1}
                              className="size-6 rounded flex items-center justify-center text-slate-400 hover:text-slate-800 disabled:opacity-25"
                              onClick={() => move(index, 1)}
                            >
                              <ArrowDown className="size-3.5" />
                            </button>
                          </div>
                        </td>
                        <td className="px-3 py-2 text-right whitespace-nowrap">
                          <button
                            type="button"
                            className="text-xs text-red-600 hover:text-red-700 hover:underline"
                            onClick={() => remove(index)}
                          >
                            Remove
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </fieldset>

      <div className="flex items-center justify-end gap-3 border-t border-slate-200 pt-4">
        <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={busy} className="bg-blue-600 text-white hover:bg-blue-700">
          {busy ? "Saving…" : "Save"}
        </Button>
      </div>
    </form>
  );
}
