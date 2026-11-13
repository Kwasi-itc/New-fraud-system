// Bridge the existing page controls to the owner's cursor pagination. Positions
// are advisory, bounded, and discarded whenever the authenticated scope changes.
let scope = "";
const books = new Map<string, Map<number, string>>();
export function resetDecisionPages(next: string) { if (scope !== next) { scope = next; books.clear(); } }

export function prepareDecisionPage(path: string) {
  const noop = { path, record: (_data: unknown) => {} };
  const url = new URL(path, "http://workspace.invalid");
  if (!url.pathname.endsWith("/decisions")) return noop;
  const offset = Number(url.searchParams.get("offset") ?? 0);
  const limit = Number(url.searchParams.get("limit") ?? 50);
  if (!Number.isSafeInteger(offset) || offset < 0 || !Number.isSafeInteger(limit) || limit < 1) return noop;
  const filters = new URLSearchParams(url.searchParams);
  for (const name of ["offset", "cursor", "limit", "include_total_count"]) filters.delete(name);
  filters.sort();
  const key = JSON.stringify([scope, url.pathname, limit, filters.toString()]);
  const positions = books.get(key) ?? new Map<number, string>();
  books.delete(key); books.set(key, positions);
  if (books.size > 32) books.delete(books.keys().next().value!);
  const explicit = url.searchParams.get("cursor");
  const recordOffset = explicit && !url.searchParams.has("offset")
    ? [...positions].find(([, value]) => value === explicit)?.[0]
    : offset;
  const cursor = explicit || positions.get(offset);
  if (cursor) { url.searchParams.set("cursor", cursor); url.searchParams.delete("offset"); }
  else if (offset === 0) url.searchParams.delete("offset");
  return {
    path: url.pathname + (url.search ? url.search : ""),
    record(data: unknown) {
      const next = (data as { pagination?: { next_cursor?: unknown } } | null)?.pagination?.next_cursor;
      if (recordOffset !== undefined && typeof next === "string" && next.length > 0 && next.length <= 3000) {
        positions.set(recordOffset + limit, next);
        if (positions.size > 256) positions.delete(positions.keys().next().value!);
      }
    },
  };
}
