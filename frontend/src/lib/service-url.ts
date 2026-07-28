export function resolveServiceUrl(
  configuredUrl: string | undefined,
  port: number
): string {
  const normalizedUrl = configuredUrl?.trim().replace(/\/+$/, "");
  if (normalizedUrl) {
    return normalizedUrl;
  }

  const normalizedBaseUrl = process.env.NEXT_PUBLIC_SERVICE_BASE_URL
    ?.trim()
    .replace(/\/+$/, "");
  if (normalizedBaseUrl) {
    try {
      const url = new URL(normalizedBaseUrl);
      url.port = String(port);
      url.pathname = "";
      url.search = "";
      url.hash = "";
      return url.toString().replace(/\/+$/, "");
    } catch {
      return `${normalizedBaseUrl}:${port}`;
    }
  }

  if (typeof window !== "undefined") {
    return `${window.location.protocol}//${window.location.hostname}:${port}`;
  }

  return `http://localhost:${port}`;
}
