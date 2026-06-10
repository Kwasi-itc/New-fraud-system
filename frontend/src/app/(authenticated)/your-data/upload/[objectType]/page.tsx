"use client";

import Link from "next/link";
import { useMemo, useRef, useState } from "react";
import { useParams } from "next/navigation";
import { AlertCircle, Download, Plus, Upload } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useAssembledDataModelQuery } from "@/lib/data-model-query";
import { useUploadCsvMutation, useUploadLogsQuery } from "@/lib/ingestion-query";
import { cn } from "@/lib/utils";
import { useDataModelWorkspaceStore } from "@/stores/data-model-store";

export default function UploadObjectTypePage() {
  const params = useParams<{ objectType: string }>();
  const tenantId = useDataModelWorkspaceStore((state) => state.tenantId);
  const objectType = decodeURIComponent(params.objectType);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [isDragActive, setIsDragActive] = useState(false);
  const [pageError, setPageError] = useState<string | null>(null);

  const assembledModelQuery = useAssembledDataModelQuery(tenantId);
  const uploadLogsQuery = useUploadLogsQuery(tenantId, objectType);
  const uploadCsvMutation = useUploadCsvMutation(tenantId, objectType);

  const assembledTables = Object.values(assembledModelQuery.data?.data_model.tables ?? {});
  const currentTable = assembledTables.find((table) => table.name === objectType);
  const fields = useMemo(
    () =>
      currentTable
        ? Object.values(currentTable.fields).sort((a, b) => a.name.localeCompare(b.name))
        : [],
    [currentTable]
  );

  function handleTemplateDownload() {
    if (!currentTable) {
      setPageError("The selected object type is not available in the current data model.");
      return;
    }

    const headers = fields.map((field) => field.name);
    const csvContent = `${headers.join(",")}\n`;
    const blob = new Blob([csvContent], { type: "text/csv;charset=utf-8;" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `${objectType}_template.csv`;
    link.click();
    URL.revokeObjectURL(url);
  }

  async function handleUpload(file: File) {
    setPageError(null);

    if (!tenantId) {
      setPageError("Missing tenant id configuration.");
      return;
    }

    if (!file.name.toLowerCase().endsWith(".csv")) {
      setPageError("Only .csv files are supported for this upload flow.");
      return;
    }

    setSelectedFile(file);

    try {
      await uploadCsvMutation.mutateAsync({ file, mode: "create" });
    } catch (error) {
      setPageError(error instanceof Error ? error.message : "Failed to upload CSV.");
    }
  }

  const uploadLogs = uploadLogsQuery.data?.upload_logs ?? [];

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <Upload className="size-5 text-slate-950" />
        <h2 className="text-2xl font-semibold tracking-tight text-slate-950">
          Upload {objectType}
        </h2>
      </div>

      <div className="rounded-xl border border-slate-200 bg-white px-4 py-3 text-sm leading-7 text-slate-600">
        <div className="flex items-start gap-3">
          <AlertCircle className="mt-1 size-4 shrink-0 text-slate-500" />
          <p>
            You can manually add {objectType} to your Marble instance from this page by
            uploading a <span className="font-medium text-slate-900">.csv</span> file
            here. The <span className="font-medium text-slate-900">.csv</span> file must
            follow the schema defined in your data model.
          </p>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Button
          type="button"
          variant="outline"
          onClick={handleTemplateDownload}
          className="h-10 rounded-lg border-slate-200 px-5 text-sm shadow-none hover:translate-y-0"
        >
          <Download className="size-4" />
          Download template .csv
        </Button>
        <Link
          href="/your-data"
          className="inline-flex h-10 items-center rounded-lg border border-slate-200 px-4 text-sm font-medium text-slate-700 transition-colors hover:bg-slate-50"
        >
          Back to data model
        </Link>
      </div>

      <div
        onDragOver={(event) => {
          event.preventDefault();
          setIsDragActive(true);
        }}
        onDragLeave={() => setIsDragActive(false)}
        onDrop={(event) => {
          event.preventDefault();
          setIsDragActive(false);
          const file = event.dataTransfer.files?.[0];
          if (file) {
            void handleUpload(file);
          }
        }}
        className={cn(
          "flex min-h-[240px] flex-col items-center justify-center rounded-xl border border-dashed px-6 py-8 text-center",
          isDragActive ? "border-[#2563eb] bg-[#f8fbff]" : "border-slate-400/70 bg-white"
        )}
      >
        <p className="text-[1.5rem] font-medium tracking-tight text-slate-900">
          Drop your filled .csv here
        </p>
        <p className="mt-3 text-xl font-light tracking-tight text-slate-400">OR</p>
        <Button
          type="button"
          onClick={() => fileInputRef.current?.click()}
          className="mt-4 h-10 rounded-xl bg-[#1d4ed8] px-4 text-sm text-white shadow-none hover:translate-y-0 hover:bg-[#1e40af]"
        >
          <Plus className="size-4" />
          Pick a file
        </Button>
        <input
          ref={fileInputRef}
          type="file"
          accept=".csv,text/csv"
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) {
              void handleUpload(file);
            }
          }}
        />
        {selectedFile ? (
          <p className="mt-4 text-sm text-slate-600">
            Selected file: <span className="font-medium text-slate-900">{selectedFile.name}</span>
          </p>
        ) : null}
      </div>

      {pageError ? (
        <div className="rounded-xl border border-red-200 bg-red-50 px-5 py-4 text-sm text-red-700">
          {pageError}
        </div>
      ) : null}

      {uploadCsvMutation.isPending ? (
        <div className="rounded-xl border border-slate-200 bg-slate-50 px-5 py-4 text-sm text-slate-600">
          Upload accepted. The ingestion worker will process the CSV asynchronously.
        </div>
      ) : null}

      <section className="space-y-3">
        <div>
          <h3 className="text-lg font-semibold tracking-tight text-slate-950">Upload logs</h3>
          <p className="mt-1 text-sm text-slate-500">
            Track the status of CSV processing for {objectType}.
          </p>
        </div>

        {uploadLogs.length > 0 ? (
          <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
            <table className="w-full border-collapse text-left">
              <thead className="bg-slate-50">
                <tr className="border-b border-slate-200 text-sm text-slate-900">
                  <th className="px-4 py-2.5 font-semibold">File</th>
                  <th className="px-4 py-2.5 font-semibold">Status</th>
                  <th className="px-4 py-2.5 font-semibold">Rows</th>
                  <th className="px-4 py-2.5 font-semibold">Success</th>
                  <th className="px-4 py-2.5 font-semibold">Failed</th>
                  <th className="px-4 py-2.5 font-semibold">Attempts</th>
                </tr>
              </thead>
              <tbody>
                {uploadLogs.map((log, index) => (
                  <tr
                    key={log.id}
                    className={cn(
                      "border-b border-slate-100 text-sm text-slate-900 last:border-b-0",
                      index % 2 === 1 && "bg-slate-50/50"
                    )}
                  >
                    <td className="px-4 py-2.5">
                      <div>
                        <p className="font-medium">{log.filename}</p>
                        <p className="text-xs text-slate-500">{log.id}</p>
                      </div>
                    </td>
                    <td className="px-4 py-2.5 capitalize">{log.status}</td>
                    <td className="px-4 py-2.5">{log.total_rows}</td>
                    <td className="px-4 py-2.5">{log.successful_rows}</td>
                    <td className="px-4 py-2.5">{log.failed_rows}</td>
                    <td className="px-4 py-2.5">{log.attempt_count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="rounded-xl border border-dashed border-slate-300 bg-slate-50 px-5 py-4 text-sm text-slate-600">
            No upload logs yet for this object type.
          </div>
        )}
      </section>
    </div>
  );
}
