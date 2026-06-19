"use client";

import Link from "next/link";
import { useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Info, Pencil, Plus, Search, Trash2, Upload } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { decisionEngineApi } from "@/lib/decision-engine-api";

function formatListCount(values: number) {
  return `${values} value${values === 1 ? "" : "s"}`;
}

function ListModal({
  isOpen,
  title,
  primaryLabel,
  fieldLabel = "Value",
  value,
  onValueChange,
  description,
  onDescriptionChange,
  onClose,
  onSave,
  saving = false,
}: {
  isOpen: boolean;
  title: string;
  primaryLabel: string;
  fieldLabel?: string;
  value: string;
  onValueChange: (value: string) => void;
  description?: string;
  onDescriptionChange?: (value: string) => void;
  onClose: () => void;
  onSave: () => void;
  saving?: boolean;
}) {
  if (!isOpen) {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-500/30 p-6 backdrop-blur-sm">
      <div className="w-full max-w-[640px] overflow-hidden rounded-2xl bg-white shadow-[0_18px_50px_rgba(15,23,42,0.18)]">
        <div className="border-b border-slate-200 px-5 py-5 text-center">
          <h2 className="text-[18px] font-semibold text-slate-950">{title}</h2>
        </div>
        <div className="space-y-4 px-5 py-5">
          <div className="space-y-2">
            <label className="text-[15px] font-medium text-slate-950">{fieldLabel}</label>
            <Input
              value={value}
              onChange={(event) => onValueChange(event.target.value)}
              className="h-11 rounded-lg border-slate-200 text-[14px] shadow-none"
            />
          </div>
          {onDescriptionChange ? (
            <div className="space-y-2">
              <label className="text-[15px] font-medium text-slate-950">Description</label>
              <textarea
                value={description ?? ""}
                onChange={(event) => onDescriptionChange(event.target.value)}
                className="min-h-[96px] w-full rounded-lg border border-slate-200 px-3 py-2 text-[14px] text-slate-950 outline-none"
              />
            </div>
          ) : null}
        </div>
        <div className="flex justify-end gap-3 border-t border-slate-200 px-5 py-4">
          <Button
            variant="outline"
            onClick={onClose}
            className="h-10 rounded-xl border-slate-200 px-4 text-[14px] shadow-none"
          >
            Cancel
          </Button>
          <Button
            onClick={onSave}
            disabled={!value.trim() || saving}
            className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
          >
            {saving ? "Saving..." : primaryLabel}
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}

export function ListDetailPage({ listId }: { listId: string }) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const queryClient = useQueryClient();
  const [newValueOpen, setNewValueOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [editValueOpen, setEditValueOpen] = useState(false);
  const [newValue, setNewValue] = useState("");
  const [editName, setEditName] = useState("");
  const [editDescription, setEditDescription] = useState("");
  const [editValue, setEditValue] = useState("");
  const [editingEntryId, setEditingEntryId] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [importFeedback, setImportFeedback] = useState<{
    tone: "success" | "error";
    message: string;
  } | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const listQuery = useQuery({
    queryKey: ["decision-engine", "custom-list", tenantId, listId],
    queryFn: () => decisionEngineApi.getCustomList(tenantId, listId),
    enabled: Boolean(tenantId && listId),
  });
  const entriesQuery = useQuery({
    queryKey: ["decision-engine", "custom-list-entries", tenantId, listId],
    queryFn: () => decisionEngineApi.listCustomListEntriesByList(tenantId, listId),
    enabled: Boolean(tenantId && listId),
  });

  const createEntryMutation = useMutation({
    mutationFn: () =>
      decisionEngineApi.createCustomListEntry(tenantId, listId, { value: newValue.trim() }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-list-entries", tenantId, listId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-list-entries", tenantId],
      });
      setNewValue("");
      setNewValueOpen(false);
    },
  });
  const updateListMutation = useMutation({
    mutationFn: () =>
      decisionEngineApi.updateCustomList(tenantId, listId, {
        name: editName.trim(),
        description: editDescription.trim(),
        kind: listQuery.data?.custom_list.kind ?? "generic_text",
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-list", tenantId, listId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-lists", tenantId],
      });
      setEditOpen(false);
    },
  });
  const deleteEntryMutation = useMutation({
    mutationFn: (entryId: string) =>
      decisionEngineApi.deleteCustomListEntry(tenantId, listId, entryId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-list-entries", tenantId, listId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-list-entries", tenantId],
      });
    },
  });
  const updateEntryMutation = useMutation({
    mutationFn: () => {
      if (!editingEntryId) {
        throw new Error("No list item selected for editing.");
      }
      return decisionEngineApi.updateCustomListEntry(tenantId, listId, editingEntryId, {
        value: editValue.trim(),
      });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-list-entries", tenantId, listId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-list-entries", tenantId],
      });
      setEditingEntryId(null);
      setEditValue("");
      setEditValueOpen(false);
    },
  });
  const importEntriesMutation = useMutation({
    mutationFn: (file: File) => decisionEngineApi.importCustomListEntries(tenantId, listId, file),
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-list-entries", tenantId, listId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-list-entries", tenantId],
      });
      setImportFeedback({
        tone: "success",
        message:
          result.imported_count > 0
            ? `Imported ${result.imported_count} value${result.imported_count === 1 ? "" : "s"}.`
            : "No values were imported from that CSV.",
      });
    },
    onError: (error) => {
      setImportFeedback({
        tone: "error",
        message: error instanceof Error ? error.message : "CSV import failed.",
      });
    },
  });
  const deleteListMutation = useMutation({
    mutationFn: () => decisionEngineApi.deleteCustomList(tenantId, listId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-lists", tenantId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-list-entries", tenantId],
      });
      if (typeof window !== "undefined") {
        window.location.href = "/detection";
      }
    },
  });

  const customList = listQuery.data?.custom_list;
  const values = entriesQuery.data?.custom_list_entries ?? [];
  const filteredValues = useMemo(
    () => values.filter((item) => item.value.toLowerCase().includes(search.toLowerCase())),
    [search, values]
  );
  const isEmpty = values.length === 0;

  function handleCSVFile(file: File | null) {
    if (!file) {
      return;
    }
    setImportFeedback(null);
    importEntriesMutation.mutate(file);
  }

  if (!tenantId) {
    return (
      <Card className="rounded-xl border border-amber-200 bg-amber-50 shadow-none">
        <CardContent className="p-5 text-sm text-amber-800">
          Set `NEXT_PUBLIC_DATA_MODEL_TENANT_ID` to load list details.
        </CardContent>
      </Card>
    );
  }

  if (listQuery.isLoading || entriesQuery.isLoading) {
    return (
      <Card className="rounded-xl border border-slate-200 shadow-none">
        <CardContent className="p-5 text-sm text-slate-600">Loading list...</CardContent>
      </Card>
    );
  }

  if (listQuery.isError || entriesQuery.isError || !customList) {
    return (
      <Card className="rounded-xl border border-red-200 bg-red-50 shadow-none">
        <CardContent className="p-5 text-sm text-red-700">
          {listQuery.error instanceof Error
            ? listQuery.error.message
            : entriesQuery.error instanceof Error
              ? entriesQuery.error.message
              : "Failed to load list."}
        </CardContent>
      </Card>
    );
  }

  return (
    <>
      <div className="mx-auto w-full max-w-[1200px] space-y-4 px-4 sm:px-6 xl:px-8">
        <div className="border-b border-slate-200 pb-3">
          <div className="flex items-center justify-between gap-4">
            <div className="flex flex-wrap items-center gap-4 text-[15px] text-slate-600">
              <Link
                href="/detection"
                className="inline-flex size-9 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-900"
              >
                <ArrowLeft className="size-4" />
              </Link>
              <span className="font-medium text-slate-700">Detection</span>
              <span>/</span>
              <span className="font-medium text-slate-700">Lists</span>
              <span>/</span>
              <span className="font-semibold text-slate-950">{customList.name}</span>
            </div>
            <Button
              variant="outline"
              onClick={() => {
                setEditName(customList.name);
                setEditDescription(customList.description);
                setEditOpen(true);
              }}
              className="h-10 rounded-xl border-slate-200 bg-white px-4 text-[14px] shadow-none"
            >
              <Pencil className="size-4" />
              Edit
            </Button>
          </div>
        </div>

        <Card className="rounded-xl border border-slate-200 shadow-none">
          <CardContent className="flex items-center gap-3 px-5 py-4 text-[14px] text-slate-700">
            <Info className="size-4 text-slate-600" />
            <span>{customList.description || "No description provided."}</span>
          </CardContent>
        </Card>

        {!isEmpty ? (
          <Button
            variant="outline"
            className="h-10 rounded-xl border-slate-200 bg-white px-4 text-[14px] shadow-none"
            onClick={() => fileInputRef.current?.click()}
            disabled={importEntriesMutation.isPending}
          >
            <Upload className="size-4" />
            {importEntriesMutation.isPending ? "Uploading CSV..." : "Upload CSV"}
          </Button>
        ) : null}

        <div
          className="flex min-h-[160px] items-center justify-center rounded-xl border border-dashed border-slate-400 bg-white text-center"
          onDragOver={(event) => event.preventDefault()}
          onDrop={(event) => {
            event.preventDefault();
            handleCSVFile(event.dataTransfer.files?.[0] ?? null);
          }}
        >
          <div className="space-y-5">
            <p className="text-[14px] text-slate-950">Drop your CSV file here</p>
            <p className="text-[14px] text-slate-500">OR</p>
            <Button
              type="button"
              onClick={() => fileInputRef.current?.click()}
              disabled={importEntriesMutation.isPending}
              className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
            >
              <Upload className="size-4" />
              {importEntriesMutation.isPending ? "Uploading..." : "Pick a CSV file"}
            </Button>
          </div>
        </div>

        <input
          ref={fileInputRef}
          type="file"
          accept=".csv,text/csv"
          className="hidden"
          onChange={(event) => {
            handleCSVFile(event.target.files?.[0] ?? null);
            event.currentTarget.value = "";
          }}
        />

        {importFeedback ? (
          <Card
            className={
              importFeedback.tone === "error"
                ? "rounded-xl border border-red-200 bg-red-50 shadow-none"
                : "rounded-xl border border-emerald-200 bg-emerald-50 shadow-none"
            }
          >
            <CardContent
              className={
                importFeedback.tone === "error"
                  ? "p-4 text-sm text-red-700"
                  : "p-4 text-sm text-emerald-700"
              }
            >
              {importFeedback.message}
            </CardContent>
          </Card>
        ) : null}

        <div className="flex gap-3">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-4 top-1/2 size-5 -translate-y-1/2 text-slate-500" />
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Search"
              className="h-11 rounded-xl border-slate-200 pl-12 text-[14px] shadow-none"
            />
          </div>
          <Button
            onClick={() => setNewValueOpen(true)}
            className="h-11 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
          >
            <Plus className="size-4" />
            New value
          </Button>
        </div>

        <Card className="overflow-hidden rounded-xl border border-slate-200 shadow-none">
          <CardContent className="p-0">
            {filteredValues.length === 0 ? (
              <div className="flex min-h-[110px] items-center justify-center text-[14px] text-slate-950">
                {isEmpty
                  ? "This list is empty. Add a value to get started."
                  : "No values match your search."}
              </div>
            ) : (
              <table className="min-w-full text-left">
                <thead>
                  <tr className="border-b border-slate-200 bg-white text-[13px] font-semibold text-slate-950">
                    <th className="px-4 py-3">Values</th>
                    <th className="px-4 py-3 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredValues.map((value) => (
                    <tr
                      key={value.id}
                      className="border-b border-slate-100 text-[14px] text-slate-950 last:border-b-0"
                    >
                      <td className="px-4 py-3">{value.value}</td>
                      <td className="px-4 py-3">
                        <div className="flex justify-end">
                          <button
                            type="button"
                            onClick={() => {
                              setEditingEntryId(value.id);
                              setEditValue(value.value);
                              setEditValueOpen(true);
                            }}
                            className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200 text-slate-500 transition hover:bg-slate-50"
                          >
                            <Pencil className="size-4" />
                          </button>
                          <button
                            type="button"
                            onClick={() => deleteEntryMutation.mutate(value.id)}
                            className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200 text-slate-500 transition hover:bg-slate-50"
                          >
                            <Trash2 className="size-4" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </CardContent>
        </Card>

        <Button
          onClick={() => {
            if (typeof window !== "undefined" && window.confirm(`Delete list "${customList.name}"?`)) {
              deleteListMutation.mutate();
            }
          }}
          className="h-10 rounded-xl bg-[#dd3719] px-4 text-[14px] shadow-none hover:bg-[#c43014]"
        >
          <Trash2 className="size-4" />
          Delete this list
        </Button>

        <p className="text-[13px] text-slate-500">{formatListCount(values.length)}</p>
      </div>

      <ListModal
        isOpen={newValueOpen}
        title="New value"
        primaryLabel="Save"
        fieldLabel="Value"
        value={newValue}
        onValueChange={setNewValue}
        onClose={() => {
          setNewValueOpen(false);
          setNewValue("");
        }}
        onSave={() => {
          if (!newValue.trim()) {
            return;
          }
          createEntryMutation.mutate();
        }}
        saving={createEntryMutation.isPending}
      />

      <ListModal
        isOpen={editOpen}
        title="Edit list"
        primaryLabel="Save"
        fieldLabel="Name"
        value={editName}
        onValueChange={setEditName}
        description={editDescription}
        onDescriptionChange={setEditDescription}
        onClose={() => setEditOpen(false)}
        onSave={() => {
          if (!editName.trim()) {
            return;
          }
          updateListMutation.mutate();
        }}
        saving={updateListMutation.isPending}
      />

      <ListModal
        isOpen={editValueOpen}
        title="Edit value"
        primaryLabel="Save"
        fieldLabel="Value"
        value={editValue}
        onValueChange={setEditValue}
        onClose={() => {
          setEditValueOpen(false);
          setEditingEntryId(null);
          setEditValue("");
        }}
        onSave={() => {
          if (!editValue.trim() || !editingEntryId) {
            return;
          }
          updateEntryMutation.mutate();
        }}
        saving={updateEntryMutation.isPending}
      />
    </>
  );
}
