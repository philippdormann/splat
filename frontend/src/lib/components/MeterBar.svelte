<script lang="ts">
  import { cn } from "$lib/utils";

  // CSP-safe meter bar: rect width/fill are SVG presentation attributes,
  // never inline styles, so the Content-Security-Policy stays intact.
  let {
    value = 0,
    max = 100,
    fill = "var(--primary)",
    class: className = "",
  }: { value?: number; max?: number; fill?: string; class?: string } = $props();

  const pct = $derived(Math.max(0, Math.min(100, max > 0 ? (value / max) * 100 : 0)));
</script>

<svg
  class={cn("h-1.5 w-full", className)}
  preserveAspectRatio="none"
  role="img"
  aria-label="{value} of {max}"
>
  <rect width="100%" height="100%" rx="3" fill="var(--muted)" />
  <rect width="{pct}%" height="100%" rx="3" fill={fill} />
</svg>
