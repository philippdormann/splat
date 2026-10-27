<script lang="ts">
  import { onMount } from "svelte";
  import { api, type TraceDetail } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import DateTime from "$lib/components/DateTime.svelte";
  import { ArrowLeft } from "lucide-svelte";

  export let id: string;
  export let traceId: string;
  export let navigate: (path: string) => void;

  let data: TraceDetail | null = null;
  let error = "";

  onMount(async () => {
    try {
      data = await api.projectTrace(id, traceId);
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load trace.";
    }
  });

  function fmtDuration(ms: number): string {
    if (ms < 1) return `${ms.toFixed(2)} ms`;
    if (ms < 1000) return `${ms.toFixed(1)} ms`;
    return `${(ms / 1000).toFixed(2)} s`;
  }

  // Depth of a span in the parent chain, for label indentation. Guards
  // against parent cycles with a visited set.
  function depthOf(spanId: string, byId: Map<string, { parentSpanId: string }>): number {
    let depth = 0;
    let cur = byId.get(spanId)?.parentSpanId;
    const seen = new Set<string>([spanId]);
    while (cur && !seen.has(cur) && depth < 32) {
      seen.add(cur);
      depth += 1;
      cur = byId.get(cur)?.parentSpanId;
    }
    return depth;
  }

  $: rows = (() => {
    if (!data || data.spans.length === 0) return null;
    const byId = new Map(data.spans.map((s) => [s.spanId, s]));
    const starts = data.spans.map((s) => new Date(s.start).getTime());
    const ends = data.spans.map((s) => new Date(s.end).getTime());
    const min = Math.min(...starts);
    let max = Math.max(...ends);
    if (!(max > min)) max = min + 1;
    const ordered = [...data.spans].sort((a, b) => new Date(a.start).getTime() - new Date(b.start).getTime());
    return {
      min,
      range: max - min,
      items: ordered.map((s) => {
        const st = new Date(s.start).getTime();
        const en = new Date(s.end).getTime();
        return {
          span: s,
          depth: depthOf(s.spanId, byId),
          x: ((st - min) / (max - min)) * 100,
          w: Math.max(((en - st) / (max - min)) * 100, 0.4),
        };
      }),
    };
  })();

  function barFill(status: string): string {
    const s = (status || "").toLowerCase();
    if (s === "ok") return "var(--chart-2)";
    if (s === "" || s === "unknown") return "var(--chart-3)";
    return "var(--destructive)";
  }
</script>

<div class="mx-auto w-full max-w-6xl space-y-4">
  <button class="inline-flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground" on:click={() => navigate(`/projects/${id}`)}>
    <ArrowLeft class="h-4 w-4" /> Back to project
  </button>
  <div>
    <h1 class="text-xl font-extrabold tracking-tight md:text-2xl">Trace</h1>
    <p class="mt-1 font-mono text-xs text-muted-foreground">{traceId}</p>
  </div>

  {#if error}
    <p class="text-sm text-destructive">{error}</p>
  {:else if !data}
    <div class="space-y-3" aria-label="Loading trace">
      <div class="h-64 animate-pulse rounded-lg bg-muted"></div>
    </div>
  {:else}
    <Card cls="p-4">
      <p class="mb-2 text-sm font-medium">Spans ({data.spans.length})</p>
      {#if !rows}
        <p class="text-xs text-muted-foreground">This trace has transactions but no spans.</p>
      {:else}
        <!-- Waterfall as SVG: rect x/width/fill are presentation
             attributes, not inline styles, so they pass the
             Content-Security-Policy that forbids style attributes. -->
        <div class="flex gap-2 text-xs">
          <div class="w-48 shrink-0 space-y-1 sm:w-64">
            {#each rows.items as r}
              <!-- Indentation via non-breaking spaces: a style directive
                   would compile to an inline style attribute, which the
                   Content-Security-Policy blocks. -->
              <p class="truncate py-1" title="{r.span.op} — {r.span.description || r.span.spanId}">
                {"\u00A0\u00A0".repeat(r.depth)}<span class="font-mono text-muted-foreground">{r.span.op || "span"}</span>
                <span class="ml-1">{r.span.description || r.span.spanId.slice(0, 8)}</span>
              </p>
            {/each}
          </div>
          <svg class="min-w-0 flex-1" height={rows.items.length * 28} role="img" aria-label="Span waterfall">
            {#each rows.items as r, i}
              <rect x={`${r.x}%`} y={i * 28 + 6} width={`${r.w}%`} height="16" rx="3" fill={barFill(r.span.status)}>
                <title>{r.span.op} — {fmtDuration(r.span.durationMs)}</title>
              </rect>
              {#if r.x + r.w > 86}
                <text x={`${Math.max(r.x + r.w - 1, 4)}%`} y={i * 28 + 18} font-size="10" text-anchor="end" fill="var(--primary-foreground)">{fmtDuration(r.span.durationMs)}</text>
              {:else}
                <text x={`${r.x + r.w + 1}%`} y={i * 28 + 18} font-size="10" fill="var(--muted-foreground)">{fmtDuration(r.span.durationMs)}</text>
              {/if}
            {/each}
          </svg>
        </div>
      {/if}
    </Card>

    <Card cls="p-4">
      <p class="mb-2 text-sm font-medium">Transactions ({data.transactions.length})</p>
      <div class="divide-y divide-border rounded-md border border-border text-sm">
        {#each data.transactions as t}
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1 p-2">
            <span class="min-w-0 flex-1 truncate font-medium">{t.name}</span>
            {#if t.release}<span class="font-mono text-xs text-muted-foreground">{t.release}</span>{/if}
            {#if t.status}<span class="font-mono text-xs text-muted-foreground">{t.status}</span>{/if}
            <span class="font-mono text-xs text-muted-foreground">{fmtDuration(t.durationMs)}</span>
          </div>
        {/each}
      </div>
    </Card>

    {#if data.logs.length}
      <Card cls="p-4">
        <p class="mb-2 text-sm font-medium">Logs in this trace ({data.logs.length})</p>
        <div class="divide-y divide-border rounded-md border border-border font-mono text-xs">
          {#each data.logs as e}
            <div class="flex flex-wrap items-baseline gap-x-2 p-2">
              <span class="text-muted-foreground"><DateTime iso={e.timestamp} /></span>
              <span class="font-semibold uppercase">{e.level}</span>
              <span class="min-w-0 flex-1 break-words font-sans">{e.message}</span>
            </div>
          {/each}
        </div>
      </Card>
    {/if}
  {/if}
</div>
