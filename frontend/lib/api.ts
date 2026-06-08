type HealthcheckResponse = {
  ok: boolean;
  service: string;
  timestamp: string;
};

export async function getHealthcheck(): Promise<HealthcheckResponse> {
  const response = await fetch("/api/health", {
    method: "GET",
    headers: {
      "Content-Type": "application/json",
    },
  });

  if (!response.ok) {
    throw new Error(`Healthcheck request failed with status ${response.status}`);
  }

  return response.json() as Promise<HealthcheckResponse>;
}
