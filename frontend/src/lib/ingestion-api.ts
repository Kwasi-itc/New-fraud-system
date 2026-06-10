export type IngestionApiErrorEnvelope = {
  error: {
    code: string;
    message: string;
  };
};

export type UploadLog = {
  id: string;
  tenant_id: string;
  object_type: string;
  mode: string;
  filename: string;
  content_type: string;
  status: "pending" | "uploaded" | "processing" | "completed" | "failed";
  total_rows: number;
  successful_rows: number;
  failed_rows: number;
  attempt_count: number;
  error_message?: string | null;
};

const ingestionServiceBaseUrl =
  process.env.NEXT_PUBLIC_INGESTION_SERVICE_URL ?? "http://localhost:8081";
const ingestionServiceToken = process.env.NEXT_PUBLIC_INGESTION_SERVICE_TOKEN;

async function ingestionFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("Accept", "application/json");

  if (!(init?.body instanceof FormData) && init?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  if (ingestionServiceToken) {
    headers.set("Authorization", `Bearer ${ingestionServiceToken}`);
  }

  const response = await fetch(`${ingestionServiceBaseUrl}${path}`, {
    ...init,
    headers,
  });

  if (!response.ok) {
    const errorBody = (await response.json().catch(() => null)) as
      | IngestionApiErrorEnvelope
      | null;
    throw new Error(
      errorBody?.error.message ?? `Request failed with status ${response.status}`
    );
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return (await response.json()) as T;
}

export const ingestionApi = {
  uploadCsv: async ({
    tenantId,
    objectType,
    file,
    mode,
  }: {
    tenantId: string;
    objectType: string;
    file: File;
    mode?: "create" | "patch";
  }) => {
    const formData = new FormData();
    formData.append("file", file);

    const searchParams = new URLSearchParams();
    if (mode === "patch") {
      searchParams.set("mode", "patch");
    }

    const suffix = searchParams.toString() ? `?${searchParams.toString()}` : "";

    return ingestionFetch<{ upload_log: UploadLog }>(
      `/v1/tenants/${tenantId}/ingest/${objectType}/csv${suffix}`,
      {
        method: "POST",
        body: formData,
      }
    );
  },
  listUploadLogs: async ({
    tenantId,
    objectType,
  }: {
    tenantId: string;
    objectType: string;
  }) =>
    ingestionFetch<{ upload_logs: UploadLog[] }>(
      `/v1/tenants/${tenantId}/ingest/${objectType}/upload-logs`
    ),
  getUploadLog: async (uploadLogId: string) =>
    ingestionFetch<{ upload_log: UploadLog }>(`/v1/upload-logs/${uploadLogId}`),
};
