<script lang="ts">
  import { onMount } from "svelte";
  import { api, type TraceTransaction } from "$lib/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import * as Empty from "$lib/components/ui/empty/index.js";
  import { Root as Skeleton } from "$lib/components/ui/skeleton/index.js";
  import DateTime from "$lib/components/DateTime.svelte";
  import { TriangleAlert, Activity } from "lucide-svelte";

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

<Card.Root class="gap-0">
  <Card.Header>
    <Card.Title>Traces</Card.Title>
    <Card.Description>Recent distributed traces (transaction items). Open one for its span waterfall.</Card.Description>
  </Card.Header>
  <Card.Content>
    {#if error}
      <Alert.Root variant="destructive">
        <TriangleAlert />
        <Alert.Title>Couldn't load traces</Alert.Title>
        <Alert.Description>{error}</Alert.Description>
      </Alert.Root>
    {:else if txns === null}
      <div class="space-y-2" aria-label="Loading traces">
        <Skeleton class="h-12 w-full" />
        <Skeleton class="h-12 w-full" />
      </div>
    {:else if txns.length === 0}
      <Empty.Root class="border-0">
        <Empty.Header>
          <span class="bg-muted flex size-12 items-center justify-center rounded-xl">
            <Activity class="text-muted-foreground size-6" />
          </span>
          <Empty.Title>No traces yet</Empty.Title>
          <Empty.Description>Enable SDK tracing (<span class="font-mono">traces_sample_rate</span>) to see them here.</Empty.Description>
        </Empty.Header>
      </Empty.Root>
    {:else}
      <div class="divide-y divide-border rounded-md border border-border text-sm">
        {#each txns as t}
          <button class="flex w-full flex-wrap items-center gap-x-3 gap-y-1 p-2 text-left hover:bg-muted" on:click={() => navigate(`/projects/${projectId}/traces/${encodeURIComponent(t.traceId)}`)}>
            <span class="min-w-0 flex-1 truncate font-medium">{t.name || "(unlabeled transaction)"}</span>
            <span class="font-mono text-xs text-muted-foreground tabular-nums">{t.spanCount} spans · {fmtDuration(t.durationMs)}</span>
            {#if t.status}<span class="font-mono text-xs text-muted-foreground">{t.status}</span>{/if}
            <span class="font-mono text-xs text-muted-foreground"><DateTime iso={t.end} /></span>
          </button>
        {/each}
      </div>
    {/if}
  </Card.Content>
</Card.Root>
