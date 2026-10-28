// Helpers for keeping UI state (filters, tabs, ranges) in the URL query
// string so views are shareable and survive reloads. Uses
// history.replaceState so typing in a filter doesn't spam the history
// stack; the SPA router only tracks the query on navigate()/popstate.

export function queryParam(search: string, key: string): string | null {
  try {
    return new URLSearchParams(search).get(key);
  } catch {
    return null;
  }
}

/** Merge the given keys into the current query string (null/"" removes). */
export function replaceQuery(params: Record<string, string | null | undefined>): void {
  try {
    const sp = new URLSearchParams(window.location.search);
    for (const [k, v] of Object.entries(params)) {
      if (v == null || v === "") sp.delete(k);
      else sp.set(k, v);
    }
    const qs = sp.toString();
    window.history.replaceState(
      {},
      "",
      window.location.pathname + (qs ? `?${qs}` : ""),
    );
  } catch {
    // Non-browser context or restricted history — UI still works.
  }
}
