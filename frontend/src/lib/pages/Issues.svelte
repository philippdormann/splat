<script lang="ts">
  import { onMount } from "svelte";
  import { ChevronRight, TriangleAlert } from "lucide-svelte";
  import { api, type IssuesData } from "$lib/api";
  import { loadPersisted, savePersisted, isString } from "$lib/persisted";
  import { queryParam, replaceQuery } from "$lib/urlquery";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import * as Empty from "$lib/components/ui/empty/index.js";
  import * as Table from "$lib/components/ui/table/index.js";
  import { Root as Skeleton } from "$lib/components/ui/skeleton/index.js";
  import { Root as Separator } from "$lib/components/ui/separator/index.js";
  import LevelBadge from "$lib/components/LevelBadge.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import RelativeTime from "$lib/components/RelativeTime.svelte";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import ConfirmDialog from "$lib/components/ConfirmDialog.svelte";
  import { toast } from "svelte-sonner";

  export let navigate: (path: string) => void;
  export let urlSearch: string = "";

  // URL query params win over persisted prefs so filtered views are
  // shareable; everything still falls back to (and updates) persistence.
  function initial(key: string, persistedKey: string): string {
    return queryParam(urlSearch, key) ?? loadPersisted<string>(persistedKey, "", isString);
  }

  let data: IssuesData | null = null;
  let q = initial("q", "splat.issues.q");
  let project = initial("project", "splat.issues.project");
  let environment = initial("environment", "splat.issues.environment");
  let status = initial("status", "splat.issues.status");
  let sort = initial("sort", "splat.issues.sort");
  $: savePersisted("splat.issues.q", q);
  $: savePersisted("splat.issues.project", project);
  $: savePersisted("splat.issues.environment", environment);
  $: savePersisted("splat.issues.status", status);
  $: savePersisted("splat.issues.sort", sort);
  let page = 1;
  const initialPage = Number(queryParam(urlSearch, "page"));
  if (Number.isInteger(initialPage) && initialPage > 0) page = initialPage;
  let selected = new Set<number>();
  let error = "";
  let timer: ReturnType<typeof setTimeout>;
  let pendingBulk: string | null = null;
  let bulkBusy = false;
  let bulkMuteFor = "forever";
  let bulkMuteCount = 50;

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
    syncURL();
  }

  // Mirror the current filters into the address bar (replaceState, no
  // history spam) and remember the list URL so issue detail can go back
  // to the exact filtered view instead of a blank /issues.
  function syncURL() {
    replaceQuery({
      q: q || null,
      project: project || null,
      environment: environment || null,
      status: status || null,
      sort: sort || null,
      page: page > 1 ? String(page) : null,
    });
    try {
      sessionStorage.setItem("splat.issues.last", window.location.pathname + window.location.search);
    } catch {
      // Storage unavailable — back button falls back to /issues.
    }
  }

  function openIssue(id: number) {
    syncURL();
    navigate(`/issues/${id}`);
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
    pendingBulk = statusValue;
  }

  async function confirmBulk() {
    if (!pendingBulk || selected.size === 0) return;
    bulkBusy = true;
    const count = selected.size;
    const label = pendingBulk ? bulkCopy[pendingBulk].label : "Updated";
    try {
      let opts: { mutedUntil?: string; mutedCount?: number } | undefined;
      if (pendingBulk === "muted") {
        if (bulkMuteFor === "count") {
          const n = Math.floor(Number(bulkMuteCount));
          if (!Number.isFinite(n) || n < 1) {
            toast.error("Enter at least 1 appearance.");
            bulkBusy = false;
            return;
          }
          opts = { mutedCount: n };
        } else if (bulkMuteFor !== "forever") {
          const hours = { "24h": 24, "7d": 24 * 7, "30d": 24 * 30 }[bulkMuteFor] ?? 0;
          if (hours > 0) opts = { mutedUntil: new Date(Date.now() + hours * 3600 * 1000).toISOString() };
        }
      }
      await api.bulkIssueStatus([...selected], pendingBulk, opts);
      selected = new Set();
      pendingBulk = null;
      toast.success(`${label}d ${count} issue${count === 1 ? "" : "s"}.`);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Bulk update failed.");
      pendingBulk = null;
    } finally {
      bulkBusy = false;
    }
  }

  const bulkCopy: Record<string, { title: string; label: string }> = {
    resolved: { title: "Resolve selected issues?", label: "Resolve" },
    ignored: { title: "Ignore selected issues?", label: "Ignore" },
    muted: { title: "Mute selected issues?", label: "Mute" },
  };
  const bulkDescriptions: Record<string, string> = {
    resolved: "New occurrences will reopen them.",
    ignored: "They stay ignored permanently and will not notify.",
    muted: "They keep counting events but never reopen and never notify, until unmuted.",
  };

  function toggle(id: number, checked: boolean) {
    const s = new Set(selected);
    if (checked) s.add(id);
    else s.delete(id);
    selected = s;
  }

  function toggleAll(checked: boolean) {
    if (!data) return;
    if (checked) {
      selected = new Set(data.issues.map((i) => i.id));
    } else {
      selected = new Set();
    }
  }

  $: allVisibleSelected = !!data && data.issues.length > 0 && data.issues.every((i) => selected.has(i.id));

  onMount(load);
</script>

<div class="space-y-6">
  <div class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Issues</h1>
      <p class="text-muted-foreground mt-1 text-sm">
        {#if data === null}Loading…{:else}{data.total.toLocaleString()} issue{data.total === 1 ? "" : "s"}{/if}
      </p>
    </div>
    {#if selected.size > 0}
      <div class="flex gap-2">
        <Button size="sm" variant="outline" on:click={() => bulk("resolved")}>Resolve ({selected.size})</Button>
        <Button size="sm" variant="outline" on:click={() => bulk("ignored")}>Ignore ({selected.size})</Button>
        <Button size="sm" variant="outline" on:click={() => bulk("muted")}>Mute ({selected.size})</Button>
      </div>
    {/if}
  </div>

  <ConfirmDialog
    open={pendingBulk !== null}
    title={pendingBulk ? bulkCopy[pendingBulk].title : ""}
    description={pendingBulk ? `${selected.size} selected issue${selected.size === 1 ? "" : "s"}. ${bulkDescriptions[pendingBulk]}` : ""}
    confirmLabel={pendingBulk ? bulkCopy[pendingBulk].label : ""}
    busy={bulkBusy}
    onConfirm={confirmBulk}
    onCancel={() => { if (!bulkBusy) pendingBulk = null; }}
  >
    {#if pendingBulk === "muted"}
      <span class="mt-3 flex flex-wrap items-center gap-2">
        <select class="h-8 rounded-md border border-input bg-background px-2 text-xs" bind:value={bulkMuteFor} aria-label="Mute duration">
          <option value="forever">Mute forever</option>
          <option value="24h">Mute 24 hours</option>
          <option value="7d">Mute 7 days</option>
          <option value="30d">Mute 30 days</option>
          <option value="count">Mute next N appearances</option>
        </select>
        {#if bulkMuteFor === "count"}
          <input type="number" min="1" step="1" bind:value={bulkMuteCount} aria-label="Appearances to mute" class="h-8 w-24 rounded-md border border-input bg-background px-2 text-xs" />
        {/if}
      </span>
    {/if}
  </ConfirmDialog>

  <Card.Root class="gap-0">
    <Card.Content class="flex flex-wrap gap-2 pt-6">
      <div class="min-w-48 flex-1"><Input placeholder="Search title or exception…" bind:value={q} on:input={search} /></div>
      <select class="h-9 rounded-md border border-input bg-background px-2 text-sm" bind:value={project} on:change={() => { page = 1; load(); }} aria-label="Project filter">
        <option value="">All projects</option>
        {#if data}{#each data.projects as p}<option value={p.id}>{p.name}</option>{/each}{/if}
      </select>
      <select class="h-9 rounded-md border border-input bg-background px-2 text-sm" bind:value={status} on:change={() => { page = 1; load(); }} aria-label="Status filter">
        <option value="">Any status</option>
        <option value="unresolved">Unresolved</option>
        <option value="resolved">Resolved</option>
        <option value="ignored">Ignored</option>
        <option value="muted">Muted</option>
      </select>
      <select class="h-9 rounded-md border border-input bg-background px-2 text-sm" bind:value={environment} on:change={() => { page = 1; load(); }} aria-label="Environment filter">
        <option value="">All environments</option>
        {#if data}{#each data.environments as e}<option value={e}>{e}</option>{/each}{/if}
      </select>
      <select class="h-9 rounded-md border border-input bg-background px-2 text-sm" bind:value={sort} on:change={() => { page = 1; load(); }} aria-label="Sort order">
        <option value="">Sort: last seen</option>
        <option value="event_count">Sort: event count</option>
        <option value="first_seen">Sort: first seen</option>
      </select>
    </Card.Content>
  </Card.Root>

  {#if error}
    <Alert.Root variant="destructive">
      <TriangleAlert />
      <Alert.Title>Couldn't load issues</Alert.Title>
      <Alert.Description>{error}</Alert.Description>
    </Alert.Root>
  {/if}

  <Card.Root class="gap-0 overflow-hidden">
    {#if !data}
      <Card.Content class="space-y-2 pt-6" aria-label="Loading issues">
        <Skeleton class="h-14 w-full" />
        <Skeleton class="h-14 w-full" />
        <Skeleton class="h-14 w-full" />
      </Card.Content>
    {:else if data.issues.length === 0}
      <Card.Content class="pt-6">
        <Empty.Root class="border-0">
          <Empty.Header>
            <Empty.Title>No issues match</Empty.Title>
            <Empty.Description>Try widening the filters or a different search.</Empty.Description>
          </Empty.Header>
        </Empty.Root>
      </Card.Content>
    {:else}
      <Table.Root>
        <Table.Header>
          <Table.Row class="hover:bg-transparent">
            <Table.Head class="w-10">
              <input
                type="checkbox"
                class="accent-primary size-4"
                checked={allVisibleSelected}
                aria-label="Select all issues on this page"
                on:change={(e) => toggleAll(e.currentTarget.checked)}
              />
            </Table.Head>
            <Table.Head>Issue</Table.Head>
            <Table.Head class="hidden text-right lg:table-cell">Trend</Table.Head>
            <Table.Head class="text-right">Events</Table.Head>
            <Table.Head class="text-right">Last seen</Table.Head>
            <Table.Head class="w-8"></Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {#each data.issues as i}
            <Table.Row>
              <Table.Cell>
                <input
                  type="checkbox"
                  class="accent-primary size-4"
                  checked={selected.has(i.id)}
                  aria-label="Select {i.title}"
                  on:change={(e) => toggle(i.id, e.currentTarget.checked)}
                />
              </Table.Cell>
              <Table.Cell>
                <button class="flex min-w-0 items-center gap-2 text-left" on:click={() => openIssue(i.id)}>
                  <LevelBadge level={i.level} />
                  <span class="min-w-0">
                    <span class="block truncate font-medium hover:underline">{i.title}</span>
                    <span class="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
                      <StatusBadge status={i.status} />
                      <span>{i.projectName}</span>
                      {#if i.environment}<span>{i.environment}</span>{/if}
                    </span>
                  </span>
                </button>
              </Table.Cell>
              <Table.Cell class="hidden lg:table-cell">
                {#if i.sparkline?.length}
                  <svg width="72" height="20" viewBox="0 0 72 20" class="fill-primary ml-auto block" aria-hidden="true">
                    {#each i.sparkline as v, idx}
                      {@const max = Math.max(...i.sparkline, 1)}
                      <rect x={idx * 3} y={20 - (1.5 + (v / max) * 18)} width="2" height={1.5 + (v / max) * 18} />
                    {/each}
                  </svg>
                {/if}
              </Table.Cell>
              <Table.Cell class="text-right tabular-nums">{i.eventCount.toLocaleString()}</Table.Cell>
              <Table.Cell class="text-muted-foreground text-right whitespace-nowrap">
                <RelativeTime iso={i.lastSeen} />
              </Table.Cell>
              <Table.Cell class="text-right">
                <button aria-label="Open {i.title}" on:click={() => openIssue(i.id)}>
                  <ChevronRight class="text-muted-foreground ml-auto size-4" />
                </button>
              </Table.Cell>
            </Table.Row>
          {/each}
        </Table.Body>
      </Table.Root>
    {/if}
  </Card.Root>

  {#if data && data.totalPages > 1}
    <div class="flex items-center gap-3 text-sm">
      <Button size="sm" variant="outline" disabled={page <= 1} on:click={() => { page -= 1; load(); }}>Prev</Button>
      <Separator orientation="vertical" class="h-5" />
      <span class="text-muted-foreground tabular-nums">Page {data.page} of {data.totalPages} ({data.total.toLocaleString()})</span>
      <Separator orientation="vertical" class="h-5" />
      <Button size="sm" variant="outline" disabled={page >= data.totalPages} on:click={() => { page += 1; load(); }}>Next</Button>
    </div>
  {/if}
</div>
