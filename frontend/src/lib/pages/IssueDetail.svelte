<script lang="ts">
  import { onMount } from "svelte";
  import { api, type IssueDetail } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Badge from "$lib/components/badge.svelte";
  import Button from "$lib/components/button.svelte";

  export let id: string;
  export let navigate: (path: string) => void;

  let data: IssueDetail | null = null;
  let error = "";

  async function load() {
    try {
      data = await api.issue(id);
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load issue.";
    }
  }

  async function setStatus(status: string) {
    await api.setIssueStatus(id, status);
    await load();
  }

  onMount(load);
</script>

{#if error}
  <p class="text-sm text-destructive">{error}</p>
{:else if !data}
  <p class="text-sm text-muted-foreground">Loading…</p>
{:else}
  <div class="space-y-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <div class="flex flex-wrap items-center gap-2">
          <Badge variant={data.issue.level === "error" ? "destructive" : "secondary"}>{data.issue.level}</Badge>
          <Badge>{data.issue.status}</Badge>
          <span class="text-xs text-muted-foreground">{data.project.name} · {data.issue.environment} · {data.issue.eventCount} events</span>
        </div>
        <h2 class="mt-2 text-xl font-bold">{data.issue.title}</h2>
        <p class="mt-1 font-mono text-xs text-muted-foreground">{data.issue.exceptionType}: {data.issue.exceptionValue}</p>
      </div>
      <div class="flex gap-2">
        <Button size="sm" variant="outline" on:click={() => setStatus("resolved")}>Resolve</Button>
        <Button size="sm" variant="outline" on:click={() => setStatus("ignored")}>Ignore</Button>
        <Button size="sm" variant="outline" on:click={() => setStatus("unresolved")}>Reopen</Button>
      </div>
    </div>

    {#if data.traceId}
      <Card cls="flex flex-wrap items-center gap-2 p-3 text-sm">
        <span class="text-muted-foreground">trace_id</span>
        <code class="font-mono text-xs">{data.traceId}</code>
        {#if data.traceURL}<a class="text-primary hover:underline" href={data.traceURL} target="_blank" rel="noreferrer">Open in tracing backend</a>{/if}
      </Card>
    {/if}

    {#if data.stacktrace.length}
      <Card cls="p-4">
        <p class="mb-2 text-sm font-medium">Stacktrace</p>
        <div class="space-y-3">
          {#each data.stacktrace as f}
            <div class="rounded-md bg-muted p-2 font-mono text-xs">
              <p>{f.module ? `${f.module}.` : ""}{f.function} — {f.file}:{f.line}</p>
              {#if f.original}
                <p class="mt-0.5 text-green-600 dark:text-green-400">
                  ↳ {f.original.function ? `${f.original.function} — ` : ""}{f.original.source}:{f.original.line}{f.original.column ? `:${f.original.column}` : ""} <span class="text-muted-foreground">(via source map)</span>
                </p>
                {#if f.original.context?.length}
                  <pre class="mt-1 overflow-x-auto whitespace-pre-wrap text-muted-foreground">{f.original.context.join("\n")}</pre>
                {/if}
              {/if}
              {#if f.contextLine}<pre class="mt-1 overflow-x-auto whitespace-pre-wrap">{f.contextLine}</pre>{/if}
              {#if f.vars.length}
                <div class="mt-1 space-y-0.5">
                  {#each f.vars as v}<p><span class="text-muted-foreground">{v.key}:</span> {v.value}</p>{/each}
                </div>
              {/if}
            </div>
          {/each}
        </div>
      </Card>
    {/if}

    <div class="grid gap-3 lg:grid-cols-2">
      {#if data.tags.length}
        <Card cls="p-4"><p class="mb-2 text-sm font-medium">Tags</p>
          {#each data.tags as t}<p class="flex justify-between gap-2 font-mono text-xs"><span class="text-muted-foreground">{t.key}</span><span class="truncate">{t.value}</span></p>{/each}
        </Card>
      {/if}
      {#if data.contexts.length}
        <Card cls="p-4"><p class="mb-2 text-sm font-medium">Contexts</p>
          {#each data.contexts as c}
            <p class="mt-2 text-xs font-semibold">{c.name}</p>
            {#each c.fields as f}<p class="flex justify-between gap-2 font-mono text-xs"><span class="text-muted-foreground">{f.key}</span><span class="truncate">{f.value}</span></p>{/each}
          {/each}
        </Card>
      {/if}
      {#if data.breadcrumbs.length}
        <Card cls="p-4"><p class="mb-2 text-sm font-medium">Breadcrumbs</p>
          {#each data.breadcrumbs as b}<p class="font-mono text-xs">[{b.timestamp}] {b.type}/{b.level}: {b.message}</p>{/each}
        </Card>
      {/if}
      {#if data.user}
        <Card cls="p-4"><p class="mb-2 text-sm font-medium">User</p>
          <p class="font-mono text-xs">{data.user.username}</p>
          {#each data.user.extra as e}<p class="flex justify-between gap-2 font-mono text-xs"><span class="text-muted-foreground">{e.key}</span><span class="truncate">{e.value}</span></p>{/each}
        </Card>
      {/if}
    </div>

    {#if data.events.length > 1}
      <Card cls="p-4"><p class="mb-2 text-sm font-medium">Events ({data.events.length})</p>
        <div class="max-h-64 space-y-1 overflow-y-auto">
          {#each data.events as e}<p class="font-mono text-xs text-muted-foreground">{e.timestamp} — {e.eventId}</p>{/each}
        </div>
      </Card>
    {/if}

    <Button variant="ghost" size="sm" on:click={() => navigate("/issues")}>← Back to issues</Button>
  </div>
{/if}
