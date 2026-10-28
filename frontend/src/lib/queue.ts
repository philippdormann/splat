// Triage queue: lists stash their visible issue IDs before jumping to a
// detail page, so the detail can offer Prev/Next and Resolve-&-next
// without knowing which filtered view the user came from. Navigation by
// ID is safe — a stale entry just lands on the detail's 404 banner.
const KEY = "splat.issues.queue";

export function stashQueue(ids: number[]): void {
  try {
    sessionStorage.setItem(KEY, JSON.stringify(ids));
  } catch {
    // Storage unavailable — Prev/Next simply stay hidden.
  }
}

export function readQueue(): number[] {
  try {
    const raw = sessionStorage.getItem(KEY);
    if (!raw) return [];
    const v: unknown = JSON.parse(raw);
    if (Array.isArray(v)) return v.filter((x): x is number => typeof x === "number");
  } catch {
    // Corrupt entry — treat as empty.
  }
  return [];
}

export function dropFromQueue(id: number): void {
  stashQueue(readQueue().filter((x) => x !== id));
}
