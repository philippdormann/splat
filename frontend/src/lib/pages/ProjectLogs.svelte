<script lang="ts">
  import { onMount } from "svelte";
  import { api, type LogEntry } from "$lib/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import * as Empty from "$lib/components/ui/empty/index.js";
  import { Root as Skeleton } from "$lib/components/ui/skeleton/index.js";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import DateTime from "$lib/components/DateTime.svelte";
  import { TriangleAlert, ScrollText } from "lucide-svelte";

  export let projectId: string;
  export let navigate: (path: string) => void;

  const levels = ["", "trace", "debug", "info", "warn", "error", "fatal"];
  let level = "";
  let query = "";
  let entries: LogEntry[] | null = null;
  let error = "";
  let busy = false;

  async function load() {
    busy = true;
    error = "";
    try {
      entries = await api.projectLogs(projectId, {
        level: level || undefined,
        q: query.trim() || undefined,
        limit: 100,
      });
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load logs.";
    } finally {
      busy = false;
    }
  }

  function search(e: Event) {
    e.preventDefault();
    load();
  }

  function levelClass(lvl: string): string {
    switch (lvl) {
      case "error":
      case "fatal":
        return "text-destructive";
      case "warn":
        return "text-amber-600 dark:text-amber-400";
      case "debug":
      case "trace":
        return "text-muted-foreground";
      default:
        return "text-green-700 dark:text-green-400";
    }
  }

  onMount(load);
</script>

<Card.Root class="gap-0">
  <Card.Header>
    <Card.Title>Logs</Card.Title>
    <Card.Description>Structured log records the SDKs send (<span class="font-mono">logger.info/warn/error…</span>). Newest first.</Card.Description>
  </Card.Header>
  <Card.Content>
  <div class="space-y-3">
    <form on:submit={search} class="flex flex-wrap gap-2">
      <select class="h-9 rounded-md border border-input bg-background px-2 text-sm" bind:value={level} on:change={load} aria-label="Log level filter">
        <option value="">All levels</option>
        {#each levels.filter((l) => l) as l}<option value={l}>{l}</option>{/each}
      </select>
      <Input bind:value={query} placeholder="Search messages…" class="min-w-0 flex-1" />
      <Button type="submit" size="sm" disabled={busy}>Search</Button>
    </form>
    {#if error}
      <Alert.Root variant="destructive">
        <TriangleAlert />
        <Alert.Title>Couldn't load logs</Alert.Title>
        <Alert.Description>{error}</Alert.Description>
      </Alert.Root>
    {:else if entries === null}
      <div class="space-y-2" aria-label="Loading logs">
        <Skeleton class="h-10 w-full" />
        <Skeleton class="h-10 w-full" />
        <Skeleton class="h-10 w-full" />
      </div>
    {:else if entries.length === 0}
      <Empty.Root class="border-0">
        <Empty.Header>
          <span class="bg-muted flex size-12 items-center justify-center rounded-xl">
            <ScrollText class="text-muted-foreground size-6" />
          </span>
          <Empty.Title>No log records</Empty.Title>
          <Empty.Description>Enable SDK logs (<span class="font-mono">enable_logs</span>) to see them here.</Empty.Description>
        </Empty.Header>
      </Empty.Root>
    {:else}
      <div class="divide-y divide-border rounded-md border border-border font-mono text-xs">
        {#each entries as e}
          <div class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 p-2">
            <span class="shrink-0 text-muted-foreground"><DateTime iso={e.timestamp} /></span>
            <span class="shrink-0 font-semibold uppercase {levelClass(e.level)}">{e.level}</span>
            <span class="min-w-0 flex-1 break-words font-sans">{e.message || "(empty)"}</span>
            {#if e.traceId}
              <button class="shrink-0 text-primary hover:underline" on:click={() => navigate(`/projects/${projectId}/traces/${encodeURIComponent(e.traceId)}`)} title="Open trace">trace:{e.traceId.slice(0, 8)}</button>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
  </Card.Content>
</Card.Root>
