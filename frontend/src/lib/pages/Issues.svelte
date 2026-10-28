<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { Check, ChevronRight, EyeOff, TriangleAlert } from "lucide-svelte";
  import { api, type IssuesData } from "$lib/api";
  import { loadPersisted, savePersisted, isString } from "$lib/persisted";
  import { queryParam, replaceQuery } from "$lib/urlquery";
  import { stashQueue } from "$lib/queue";
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
  const initialPerPage = Number(queryParam(urlSearch, "per_page"));
  let perPage = [25, 50, 100].includes(initialPerPage)
    ? initialPerPage
    : loadPersisted<number>("splat.issues.perPage", 50, (v): v is number => typeof v === "number" && [25, 50, 100].includes(v));
  $: savePersisted("splat.issues.perPage", perPage);
  let selected = new Set<number>();
  let error = "";
  let newCount = 0;
  let listChanged = false;
  let pollTimer: ReturnType<typeof setInterval>;
  let timer: ReturnType<typeof setTimeout>;
  let pendingBulk: string | null = null;
  let bulkBusy = false;
  let bulkMuteFor = "forever";
  let bulkMuteCount = 50;
  let pendingQuick: { id: number; status: string; title: string } | null = null;
  let quickBusy = false;

  function params() {
    const p = new URLSearchParams();
    if (q) p.set("q", q);
    if (project) p.set("project", project);
    if (environment) p.set("environment", environment);
    if (status) p.set("status", status);
    if (sort) p.set("sort", sort);
    if (perPage !== 50) p.set("per_page", String(perPage));
    p.set("page", String(page));
    return p;
  }

  async function load() {
    newCount = 0;
    listChanged = false;
    try {
      data = await api.issues(params());
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load issues.";
    }
    syncURL();
  }

  // Background freshness check: never moves rows under the cursor, just
  // offers a pill. Silent on failure — triage shouldn't flicker.
  async function checkNew() {
    if (document.hidden || !data) return;
    try {
      const p = params();
      p.set("page", "1");
      const fresh = await api.issues(p);
      if (page === 1) {
        const known = new Set(data.issues.map((i) => i.id));
        newCount = fresh.issues.filter((f) => !known.has(f.id)).length;
      } else if (fresh.total !== data.total) {
        listChanged = true;
      }
    } catch {
      // Ignore — the next poll retries.
    }
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
      per_page: perPage !== 50 ? String(perPage) : null,
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
    if (data) stashQueue(data.issues.map((i) => i.id));
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

  const quickCopy: Record<string, { title: string; label: string }> = {
    resolved: { title: "Resolve issue?", label: "Resolve" },
    ignored: { title: "Ignore issue?", label: "Ignore" },
  };

  async function confirmQuick() {
    if (!pendingQuick) return;
    quickBusy = true;
    try {
      await api.setIssueStatus(pendingQuick.id, pendingQuick.status);
      toast.success(`${quickCopy[pendingQuick.status].label}d “${pendingQuick.title}”.`);
      const s = new Set(selected);
      s.delete(pendingQuick.id);
      selected = s;
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update issue.");
    } finally {
      quickBusy = false;
      pendingQuick = null;
    }
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

  // Windowed page buttons: 1 … p-2 p-1 p p+1 p+2 … last.
  $: pageItems = ((): (number | string)[] => {
    const t = data?.totalPages ?? 0;
    if (t <= 1) return [];
    const keep = new Set(
      [1, t, page - 2, page - 1, page, page + 1, page + 2].filter((n) => n >= 1 && n <= t),
    );
    const out: (number | string)[] = [];
    let prev = 0;
    for (const n of [...keep].sort((a, b) => a - b)) {
      if (n - prev > 1) out.push("…");
      out.push(n);
      prev = n;
    }
    return out;
  })();

  onMount(() => {
    load();
    pollTimer = setInterval(checkNew, 30_000);
  });
  onDestroy(() => clearInterval(pollTimer));
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

  <ConfirmDialog
    open={pendingQuick !== null}
    title={pendingQuick ? quickCopy[pendingQuick.status].title : ""}
    description={pendingQuick ? `“${pendingQuick.title}”` : ""}
    confirmLabel={pendingQuick ? quickCopy[pendingQuick.status].label : ""}
    busy={quickBusy}
    onConfirm={confirmQuick}
    onCancel={() => { if (!quickBusy) pendingQuick = null; }}
  />

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
    {#if newCount > 0 || listChanged}
      <div class="flex justify-center border-b border-border py-2">
        <Button size="sm" variant="outline" on:click={() => { page = 1; load(); }}>
          {newCount > 0 ? `${newCount} new issue${newCount === 1 ? "" : "s"} — show` : "List changed — refresh"}
        </Button>
      </div>
    {/if}
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
            <Table.Row class="group">
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
                <span class="flex min-w-0 items-center gap-2">
                  <button class="flex min-w-0 flex-1 items-center gap-2 text-left" on:click={() => openIssue(i.id)}>
                    <LevelBadge level={i.level} />
                    <span class="min-w-0">
                      <span class="block truncate font-medium hover:underline">{i.title}</span>
                      <span class="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
                        <StatusBadge status={i.status} />
                        <span>{i.projectName}</span>
                        {#if i.environment}<span>{i.environment}</span>{/if}
                        <span>open <RelativeTime iso={i.firstSeen} /></span>
                      </span>
                    </span>
                  </button>
                  <span class="hidden shrink-0 items-center gap-1 group-hover:flex focus-within:flex">
                    <button
                      class="rounded-md p-1.5 text-green-700 hover:bg-green-50 dark:text-green-400 dark:hover:bg-green-950"
                      title="Resolve {i.title}"
                      aria-label="Resolve {i.title}"
                      on:click={() => (pendingQuick = { id: i.id, status: "resolved", title: i.title })}
                    ><Check class="size-4" /></button>
                    <button
                      class="text-muted-foreground rounded-md p-1.5 hover:bg-muted hover:text-foreground"
                      title="Ignore {i.title}"
                      aria-label="Ignore {i.title}"
                      on:click={() => (pendingQuick = { id: i.id, status: "ignored", title: i.title })}
                    ><EyeOff class="size-4" /></button>
                  </span>
                </span>
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

  {#if data && data.total > 0}
    <div class="flex flex-wrap items-center gap-2 text-sm">
      <Button size="sm" variant="outline" disabled={page <= 1} on:click={() => { page -= 1; load(); }}>Prev</Button>
      {#each pageItems as n}
        {#if typeof n === "number"}
          <Button size="sm" variant={n === page ? "default" : "outline"} on:click={() => { page = n; load(); }}>{n}</Button>
        {:else}
          <span class="text-muted-foreground px-1">…</span>
        {/if}
      {/each}
      <Button size="sm" variant="outline" disabled={page >= data.totalPages} on:click={() => { page += 1; load(); }}>Next</Button>
      <Separator orientation="vertical" class="h-5" />
      <span class="text-muted-foreground tabular-nums">{data.total.toLocaleString()} issue{data.total === 1 ? "" : "s"}</span>
      <Separator orientation="vertical" class="h-5" />
      <label class="text-muted-foreground flex items-center gap-1.5 text-xs">
        Per page
        <select class="h-8 rounded-md border border-input bg-background px-1.5 text-xs text-foreground" bind:value={perPage} on:change={() => { page = 1; load(); }} aria-label="Issues per page">
          <option value={25}>25</option>
          <option value={50}>50</option>
          <option value={100}>100</option>
        </select>
      </label>
    </div>
  {/if}
</div>
