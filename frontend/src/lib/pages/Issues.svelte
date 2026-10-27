<script lang="ts">
  import { onMount } from "svelte";
  import { api, type IssuesData } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Badge from "$lib/components/badge.svelte";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";

  export let navigate: (path: string) => void;

  let data: IssuesData | null = null;
  let q = "";
  let project = "";
  let environment = "";
  let status = "";
  let sort = "";
  let page = 1;
  let selected = new Set<number>();
  let error = "";
  let timer: ReturnType<typeof setTimeout>;

  function params() {
    const p = new URLSearchParams();
    if (q) p.set("q", q);
    if (project) p.set("project", project);
    if (environment) p.set("environment", environment);
    if (status) p.set("status", status);
    if (sort) p.set("sort", sort);
    p.set("page", String(page));
    return p;
  }

  async function load() {
    try {
      data = await api.issues(params());
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load issues.";
    }
  }

  function search() {
    clearTimeout(timer);
    timer = setTimeout(() => {
      page = 1;
      load();
    }, 250);
  }

  async function bulk(statusValue: string) {
    if (selected.size === 0) return;
    await api.bulkIssueStatus([...selected], statusValue);
    selected = new Set();
    await load();
  }

  onMount(load);
</script>

<div class="space-y-4">
  <div class="flex flex-wrap items-center justify-between gap-3">
    <h2 class="text-2xl font-bold">Issues</h2>
    {#if selected.size > 0}
      <div class="flex gap-2">
        <Button size="sm" variant="outline" on:click={() => bulk("resolved")}>Resolve ({selected.size})</Button>
        <Button size="sm" variant="outline" on:click={() => bulk("ignored")}>Ignore ({selected.size})</Button>
      </div>
    {/if}
  </div>

  <Card cls="flex flex-wrap gap-2 p-3">
    <div class="min-w-48 flex-1"><Input placeholder="Search title or exception…" bind:value={q} on:input={search} /></div>
    <select class="h-9 rounded-md border border-input bg-background px-2 text-sm" bind:value={project} on:change={() => { page = 1; load(); }}>
      <option value="">All projects</option>
      {#if data}{#each data.projects as p}<option value={p.id}>{p.name}</option>{/each}{/if}
    </select>
    <select class="h-9 rounded-md border border-input bg-background px-2 text-sm" bind:value={status} on:change={() => { page = 1; load(); }}>
      <option value="">Any status</option>
      <option value="unresolved">Unresolved</option>
      <option value="resolved">Resolved</option>
      <option value="ignored">Ignored</option>
    </select>
    <select class="h-9 rounded-md border border-input bg-background px-2 text-sm" bind:value={sort} on:change={() => { page = 1; load(); }}>
      <option value="">Sort: last seen</option>
      <option value="event_count">Sort: event count</option>
      <option value="first_seen">Sort: first seen</option>
    </select>
  </Card>

  {#if error}<p class="text-sm text-destructive">{error}</p>{/if}

  <Card cls="divide-y divide-border">
    {#if !data}
      <p class="p-4 text-sm text-muted-foreground">Loading…</p>
    {:else if data.issues.length === 0}
      <p class="p-4 text-sm text-muted-foreground">No issues match.</p>
    {:else}
      {#each data.issues as i}
        <div class="flex items-center gap-3 p-3">
          <input type="checkbox" checked={selected.has(i.id)} on:change={(e) => { const s = new Set(selected); if (e.currentTarget.checked) s.add(i.id); else s.delete(i.id); selected = s; }} />
          <button class="flex flex-1 items-center justify-between gap-3 text-left" on:click={() => navigate(`/issues/${i.id}`)}>
            <span>
              <span class="block truncate text-sm font-medium">{i.title}</span>
              <span class="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <Badge variant={i.level === "error" ? "destructive" : "secondary"}>{i.level}</Badge>
                <span>{i.projectName}</span>
                {#if i.environment}<span>{i.environment}</span>{/if}
                <span>{i.eventCount} events · {i.lastSeenRelative}</span>
              </span>
            </span>
            {#if i.sparkline?.length}
              <svg width="72" height="20" viewBox="0 0 72 20" class="shrink-0 fill-primary" aria-hidden="true">
                {#each i.sparkline as v, idx}
                  {@const max = Math.max(...i.sparkline, 1)}
                  <rect x={idx * 3} y={20 - (1.5 + (v / max) * 18)} width="2" height={1.5 + (v / max) * 18} />
                {/each}
              </svg>
            {/if}
          </button>
        </div>
      {/each}
    {/if}
  </Card>

  {#if data && data.totalPages > 1}
    <div class="flex items-center gap-2 text-sm">
      <Button size="sm" variant="outline" disabled={page <= 1} on:click={() => { page -= 1; load(); }}>Prev</Button>
      <span class="text-muted-foreground">Page {data.page} of {data.totalPages} ({data.total})</span>
      <Button size="sm" variant="outline" disabled={page >= data.totalPages} on:click={() => { page += 1; load(); }}>Next</Button>
    </div>
  {/if}
</div>
