"use client";

import { useState } from "react";
import { CheckCircle2, UploadCloud } from "lucide-react";
import { useCaseWrite } from "@/lib/case-manager-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Problem, labelClass } from "./shared";
import { cn } from "@/lib/utils";

export function FileUpload({
  tenant,
  session,
  caseId,
}: {
  tenant: string;
  session: string;
  caseId: string;
}) {
  const write = useCaseWrite(tenant, session);
  const [file, setFile] = useState<File>();
  const [uploadId, setUploadId] = useState<string>();
  const [uploaded, setUploaded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [success, setSuccess] = useState(false);

  return (
    <form
      className="space-y-3 rounded-xl border border-dashed border-slate-300 bg-slate-50/50 p-4 transition-colors hover:border-slate-400"
      onSubmit={async (event) => {
        event.preventDefault();
        if (!file) return;
        const form = event.currentTarget;
        setBusy(true);
        setError(null);
        setSuccess(false);
        try {
          if (!file.size || file.size > 10 * 1024 * 1024)
            throw new Error("Choose a file between 1 byte and 10 MiB.");
          let id = uploadId;
          if (!id) {
            const result = (await write.mutateAsync({
              path: `cases/${caseId}/uploads`,
              payload: {
                file_name: file.name,
                file_size: file.size,
                content_type: file.name.toLowerCase().endsWith(".csv")
                  ? "text/csv"
                  : file.type || "text/plain",
              },
            })) as { upload: { id: string } };
            id = result.upload.id;
            setUploadId(id);
          }
          if (!uploaded) {
            const response = await fetch(
              `/api/cases/tenants/${tenant}/cases/${caseId}/uploads/${id}/content`,
              {
                method: "PUT",
                headers: { "Content-Type": "application/octet-stream" },
                body: file,
                credentials: "same-origin",
              }
            );
            if (!response.ok) {
              const data = await response.json();
              throw new Error(data.error ?? "Upload failed.");
            }
            setUploaded(true);
          }
          await write.mutateAsync({ path: `cases/${caseId}/uploads/${id}/finalize` });
          setFile(undefined);
          setUploadId(undefined);
          setUploaded(false);
          setSuccess(true);
          form.reset();
        } catch (cause) {
          setError(cause instanceof Error ? cause : new Error("Upload failed."));
        } finally {
          setBusy(false);
        }
      }}
    >
      <div className="flex items-center gap-2">
        <UploadCloud className="size-4 text-blue-600" />
        <p className="text-sm text-slate-500">
          PDF, PNG, JPEG, plain text or CSV, up to 10 MiB. The case must be open.
        </p>
      </div>

      <label className={labelClass}>
        <span>Evidence file</span>
        <Input
          type="file"
          required
          disabled={busy}
          accept=".pdf,.png,.jpg,.jpeg,.txt,.csv"
          onChange={(event) => {
            setFile(event.target.files?.[0]);
            setUploadId(undefined);
            setUploaded(false);
            setSuccess(false);
            setError(null);
          }}
          className="font-normal normal-case cursor-pointer text-sm file:mr-3 file:py-1 file:px-2.5 file:rounded-md file:border-0 file:text-sm file:font-semibold file:bg-blue-50 file:text-blue-700 hover:file:bg-blue-100"
        />
      </label>

      <Problem error={error} />
      {success && (
        <p
          role="status"
          className="flex items-center gap-1.5 rounded-lg bg-emerald-50 p-2 text-sm font-medium text-emerald-800 border border-emerald-200"
        >
          <CheckCircle2 className="size-3.5 text-emerald-600" />
          Evidence attached.
        </p>
      )}

      <Button
        type="submit"
        size="sm"
        disabled={busy || !file}
        className="text-sm font-medium"
      >
        {busy
          ? "Uploading…"
          : uploadId
          ? "Retry attachment"
          : "Upload evidence"}
      </Button>
    </form>
  );
}
