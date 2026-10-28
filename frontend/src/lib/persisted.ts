// localStorage-backed UI preferences.
//
// Keeps filter/tab selections across reloads and navigation (dashboard range,
// issues filters, project tab, settings tab, ...). Plain load + reactive save
// so it works with `bind:value` without store ceremony. Values are JSON
// encoded; unreadable or invalid entries fall back to the default.

export function loadPersisted<T>(key: string, fallback: T, validate?: (v: unknown) => v is T): T {
  try {
    const raw = localStorage.getItem(key);
    if (raw === null) return fallback;
    const value: unknown = JSON.parse(raw);
    if (validate && !validate(value)) return fallback;
    return value as T;
  } catch {
    return fallback;
  }
}

export function savePersisted(key: string, value: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Storage unavailable (private mode, quota, SSR) — UI still works.
  }
}

export function isStringIn(list: readonly string[]): (v: unknown) => v is string {
  return (v: unknown): v is string => typeof v === "string" && list.includes(v);
}

export function isString(v: unknown): v is string {
  return typeof v === "string";
}
