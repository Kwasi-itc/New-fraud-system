"use client";

import Link from "next/link";
import { useState } from "react";
import { createPortal } from "react-dom";
import { ArrowLeft, Info, Pencil, Plus, Search, Trash2, Upload } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { formatListCount, getDetectionList } from "@/components/detection/mock-lists";

function NewValueModal({
  isOpen,
  value,
  setValue,
  onClose,
  onSave,
}: {
  isOpen: boolean;
  value: string;
  setValue: (value: string) => void;
  onClose: () => void;
  onSave: () => void;
}) {
  if (!isOpen) {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-500/30 p-6 backdrop-blur-sm">
      <div className="w-full max-w-[640px] overflow-hidden rounded-2xl bg-white shadow-[0_18px_50px_rgba(15,23,42,0.18)]">
        <div className="border-b border-slate-200 px-5 py-5 text-center">
          <h2 className="text-[18px] font-semibold text-slate-950">New value</h2>
        </div>
        <div className="space-y-4 px-5 py-5">
          <div className="space-y-2">
            <label className="text-[15px] font-medium text-slate-950">Value</label>
            <Input
              value={value}
              onChange={(event) => setValue(event.target.value)}
              placeholder="e.g. 10.0.0.1 or 192.168.0.0/24"
              className="h-11 rounded-lg border-slate-200 text-[14px] shadow-none"
            />
          </div>
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
            disabled={!value.trim()}
            className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
          >
            Save
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}

export function ListDetailPage({ listId }: { listId: string }) {
  const baseList = getDetectionList(listId);
  const [values, setValues] = useState(baseList.values);
  const [newValueOpen, setNewValueOpen] = useState(false);
  const [newValue, setNewValue] = useState("");
  const [search, setSearch] = useState("");
  const isEmpty = values.length === 0;
  const filteredValues = values.filter((value) =>
    value.toLowerCase().includes(search.toLowerCase())
  );

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
              <span className="font-semibold text-slate-950">{baseList.name}</span>
            </div>
            <Button
              variant="outline"
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
            <span>{baseList.description}</span>
          </CardContent>
        </Card>

        {!isEmpty ? (
          <Button
            variant="outline"
            className="h-10 rounded-xl border-slate-200 bg-white px-4 text-[14px] shadow-none"
          >
            <Upload className="size-4" />
            Download values as CSV
          </Button>
        ) : null}

        <div className="flex min-h-[160px] items-center justify-center rounded-xl border border-dashed border-slate-400 bg-white text-center">
          <div className="space-y-5">
            <p className="text-[14px] text-slate-950">Drop your CSV file here</p>
            <p className="text-[14px] text-slate-500">OR</p>
            <Button className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]">
              <Upload className="size-4" />
              Pick a CSV file
            </Button>
          </div>
        </div>

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
                      key={value}
                      className="border-b border-slate-100 text-[14px] text-slate-950 last:border-b-0"
                    >
                      <td className="px-4 py-3">{value}</td>
                      <td className="px-4 py-3">
                        <div className="flex justify-end">
                          <button
                            type="button"
                            onClick={() =>
                              setValues((current) => current.filter((item) => item !== value))
                            }
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

        <Button className="h-10 rounded-xl bg-[#dd3719] px-4 text-[14px] shadow-none hover:bg-[#c43014]">
          <Trash2 className="size-4" />
          Delete this list
        </Button>

        <p className="text-[13px] text-slate-500">{formatListCount(values)}</p>
      </div>

      <NewValueModal
        isOpen={newValueOpen}
        value={newValue}
        setValue={setNewValue}
        onClose={() => {
          setNewValueOpen(false);
          setNewValue("");
        }}
        onSave={() => {
          if (!newValue.trim()) {
            return;
          }

          setValues((current) => [newValue.trim(), ...current]);
          setNewValueOpen(false);
          setNewValue("");
        }}
      />
    </>
  );
}
