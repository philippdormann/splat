import { readable } from "svelte/store";

// Shared clock: re-renders every 30s so relative times keep ticking
// without a reload. Single interval no matter how many timestamps show.
export const now = readable(Date.now(), (set) => {
  const t = setInterval(() => set(Date.now()), 30_000);
  return () => clearInterval(t);
});

export function formatRelative(iso: string, at: number): string {
  const d = at - new Date(iso).getTime();
  if (Number.isNaN(d) || d < 0) return "just now";
  const m = Math.floor(d / 60_000);
  if (m < 1) return "just now";
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const days = Math.floor(h / 24);
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  if (months < 12) return `${months}mo ago`;
  return `${Math.floor(months / 12)}y ago`;
}

// Absolute time in the viewer's locale, e.g. "10/27/2026, 11:05:00 AM".
export function formatLocal(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

// Canonical UTC form for tooltips, e.g. "2026-10-27T11:05:00Z".
export function formatUTC(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toISOString().replace(/\.\d{3}Z$/, "Z");
}
