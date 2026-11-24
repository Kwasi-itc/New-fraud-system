export async function accountRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/auth/${path}`, { ...init, cache: "no-store", headers: { "Content-Type": "application/json", ...init?.headers } });
  if (!response.ok) {
    let message = "The account request could not complete. Refresh and retry.";
    const body = await response.text();
    try {
      const data = JSON.parse(body);
      if (typeof data.error === "string") message = data.error.slice(0, 300);
    } catch {
      const plain = body.trim().replace(/[\r\n\t]/g, " ").slice(0, 300);
      if (plain) message = plain;
    }
    throw new Error(message);
  }
  return response.status === 204 ? undefined as T : response.json();
}
