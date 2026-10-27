<script lang="ts">
  import { onMount } from "svelte";
  import { api, type TraceTransaction } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import DateTime from "$lib/components/DateTime.svelte";

  export let projectId: string;
  export let navigate: (path: string) => void;

  let txns: TraceTransaction[] | null = null;
  let error = "";

  onMount(async () => {
    try {
      txns = await api.projectTraces(projectId, 50);
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load traces.";
    }
  });

  function fmtDuration(ms: number): string {
    if (ms < 1) return `${ms.toFixed(2)} ms`;
    if (ms < 1000) return `${ms.toFixed(1)} ms`;
    return `${(ms / 1000).toFixed(2)} s`;
  }
</script>

<Card cls="p-4">
  <div class="space-y-3">
    <p class="text-sm font-medium">Traces</p>
    <p class="text-xs text-muted-foreground">Recent distributed traces (transaction items). Open one for its span waterfall.</p>
    {#if error}
      <p class="text-sm text-destructive">{error}</p>
    {:else if txns === null}
      <p class="text-xs text-muted-foreground">Loading…</p>
    {:else if txns.length === 0}
      <p class="text-xs text-muted-foreground">No traces yet — enable SDK tracing (<span class="font-mono">traces_sample_rate</span>) to see them here.</p>
    {:else}
      <div class="divide-y divide-border rounded-md border border-border text-sm">
        {#each txns as t}
          <button class="flex w-full flex-wrap items-center gap-x-3 gap-y-1 p-2 text-left hover:bg-muted" on:click={() => navigate(`/projects/${projectId}/traces/${encodeURIComponent(t.traceId)}`)}>
            <span class="min-w-0 flex-1 truncate font-medium">{t.name || "(unlabeled transaction)"}</span>
            <span class="font-mono text-xs text-muted-foreground">{t.spanCount} spans · {fmtDuration(t.durationMs)}</span>
            {#if t.status}<span class="font-mono text-xs text-muted-foreground">{t.status}</span>{/if}
            <span class="font-mono text-xs text-muted-foreground"><DateTime iso={t.end} /></span>
          </button>
        {/each}
      </div>
    {/if}
  </div>
</Card>
