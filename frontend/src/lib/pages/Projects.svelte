<script lang="ts">
  import { onMount } from "svelte";
  import { api, ApiError, type ProjectListItem, type ProjectsPage } from "$lib/api";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import * as Empty from "$lib/components/ui/empty/index.js";
  import * as Table from "$lib/components/ui/table/index.js";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";
  import { Root as Checkbox } from "$lib/components/ui/checkbox/index.js";
  import { Root as Skeleton } from "$lib/components/ui/skeleton/index.js";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import PlatformIcon from "$lib/components/PlatformIcon.svelte";
  import TrendSparkline from "$lib/components/TrendSparkline.svelte";
  import DeleteProjectsDialog from "$lib/components/DeleteProjectsDialog.svelte";
  import TablePagination from "$lib/components/TablePagination.svelte";
  import { toast } from "svelte-sonner";
  import { FolderOpen, Plus, Trash2, MoreVertical, Search, TriangleAlert, LoaderCircle, ArrowUpDown } from "lucide-svelte";

  export let navigate: (path: string) => void;

  const PER_PAGE = 25;

  let items: ProjectListItem[] = [];
  let totalCount = 0;
  let totalPages = 1;
  let page = 1;
  let query = "";
  let queryTimer: ReturnType<typeof setTimeout> | null = null;
  let loading = true;
  let error = "";
  let busy = false;
  let selected: Set<number> = new Set();
  let pendingDelete: ProjectListItem | null = null;
  let deleteOpen = false;

  $: allSelected = items.length > 0 && items.every((p) => selected.has(p.id));
  $: someSelected = items.some((p) => selected.has(p.id));
  $: shown = items.filter((p) => selected.size === 0 || true);

  async function load(resetPage = false) {
    if (resetPage) page = 1;
    loading = true;
    error = "";
    try {
      const res: ProjectsPage = await api.projects({ page, perPage: PER_PAGE, query: query.trim() || undefined, statsPeriod: "24h" });
      items = res.items;
      totalCount = res.total_count;
      totalPages = res.total_pages;
      page = res.page;
      // Drop selections for rows no longer on screen.
      const ids = new Set(items.map((p) => p.id));
      selected = new Set([...selected].filter((id) => ids.has(id)));
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load projects.";
    } finally {
      loading = false;
    }
  }

  function onSearchInput() {
    if (queryTimer) clearTimeout(queryTimer);
    queryTimer = setTimeout(() => load(true), 300);
  }

  function toggleAll(checked: boolean) {
    if (checked) selected = new Set(items.map((p) => p.id));
    else selected = new Set();
  }

  function toggleOne(id: number, checked: boolean) {
    const next = new Set(selected);
    if (checked) next.add(id);
    else next.delete(id);
    selected = next;
  }

  type Health = "critical" | "rising" | "quiet";
  function healthOf(p: ProjectListItem): Health {
    const s = p.stats;
    if (!s) return "quiet";
    if (s.fatal_issues > 0) return "critical";
    if ((s.new_issues.previous ?? 0) !== null && s.new_issues.current > (s.new_issues.previous ?? 0)) return "rising";
    return "quiet";
  }
  function healthLabel(h: Health): string {
    return h === "critical" ? "critical" : h === "rising" ? "rising" : "quiet";
  }
  function compact(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
    if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
    return String(n);
  }
  function deltaText(current: number, previous: number): { text: string; bad: boolean } {
    const d = current - previous;
    if (d === 0) return { text: "±0", bad: false };
    return { text: d > 0 ? `+${compact(d)}` : `−${compact(-d)}`, bad: d > 0 };
  }
  function relTime(iso: string | undefined): string {
    if (!iso) return "—";
    const d = Date.now() - new Date(iso).getTime();
    if (d < 60000) return "just now";
    if (d < 3600000) return `${Math.round(d / 60000)}m ago`;
    if (d < 86400000) return `${Math.round(d / 3600000)}h ago`;
    return `${Math.round(d / 86400000)}d ago`;
  }

  async function confirmDelete() {
    const ids = pendingDelete ? [pendingDelete.id] : [...selected];
    if (ids.length === 0) return;
    deleteOpen = false;
    busy = true;
    // Optimistic removal for immediate feedback.
    const backup = items;
    items = items.filter((p) => !ids.includes(p.id));
    try {
      for (const id of ids) {
        await api.deleteProject(id);
      }
      toast.success(ids.length === 1 ? "Project deleted." : `${ids.length} projects deleted.`);
      if (!pendingDelete) selected = new Set();
      pendingDelete = null;
      await load();
      if (items.length === 0 && page > 1) await load();
    } catch (e) {
      items = backup;
      toast.error(e instanceof Error ? e.message : "Delete failed.");
      await load();
    } finally {
      busy = false;
    }
  }

  onMount(() => load(true));
</script>

<div class="flex h-full flex-col space-y-4">
  <div class="flex flex-wrap items-start justify-between gap-3">
    <div>
      <h1 class="text-xl font-extrabold tracking-tight md:text-2xl">Projects</h1>
      <p class="text-muted-foreground mt-1 text-sm">
        {#if loading}Loading…{:else if totalCount === 0}Nothing here yet{:else}{totalCount} project{totalCount === 1 ? "" : "s"}{/if}
      </p>
    </div>
    <Button size="sm" on:click={() => navigate("/projects/new")}><Plus class="mr-1 size-4" /> New project</Button>
  </div>

  <div class="relative w-full max-w-sm">
    <Search class="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
    <Input bind:value={query} on:input={onSearchInput} placeholder="Search projects…" autocomplete="off" class="pl-8" />
  </div>

  {#if selected.size > 0}
    <div class="flex items-center justify-end gap-2">
      <span class="text-sm text-muted-foreground">{selected.size} selected</span>
      <Button
        size="sm"
        variant="destructive"
        disabled={busy}
        on:click={() => { pendingDelete = null; deleteOpen = true; }}
      ><Trash2 class="mr-1 size-3" /> Delete</Button>
    </div>
  {/if}

  {#if error}
    <Alert.Root variant="destructive">
      <TriangleAlert />
      <Alert.Title>Couldn't load projects</Alert.Title>
      <Alert.Description>{error}</Alert.Description>
    </Alert.Root>
  {:else if loading && items.length === 0}
    <div class="overflow-hidden rounded-lg border">
      {#each Array(5) as _}
        <div class="flex items-center gap-4 border-b px-4 py-4 last:border-b-0">
          <Skeleton class="size-4" />
          <Skeleton class="size-7 rounded-md" />
          <div class="flex-[3] space-y-2"><Skeleton class="h-4 w-32" /><Skeleton class="h-3 w-24" /></div>
          <Skeleton class="hidden h-4 w-20 sm:block" />
          <Skeleton class="hidden h-4 w-24 md:block" />
        </div>
      {/each}
    </div>
  {:else if items.length === 0}
    <div class="flex flex-1 flex-col items-center justify-center py-16 text-center">
      <FolderOpen class="text-muted-foreground/50 mb-4 size-12" />
      {#if query.trim()}
        <p class="text-muted-foreground">No projects match “{query.trim()}”.</p>
      {:else}
        <p class="text-muted-foreground">No projects yet.</p>
        <p class="text-muted-foreground/70 text-sm">Create one to get a DSN and setup snippet.</p>
        <Button class="mt-4" on:click={() => navigate("/projects/new")}><Plus class="mr-2 size-4" /> New project</Button>
      {/if}
    </div>
  {:else}
    <div class="flex-1 overflow-hidden rounded-lg border">
      <div class="flex items-center gap-4 border-b bg-muted/50 px-4 py-3 text-xs font-bold uppercase tracking-widest text-muted-foreground">
        <Checkbox
          checked={allSelected}
          indeterminate={!allSelected && someSelected}
          onCheckedChange={(c: boolean | "indeterminate") => toggleAll(c === true)}
          aria-label="Select all projects"
        />
        <span class="w-7 shrink-0" aria-hidden />
        <span class="min-w-0 flex-[3]">Project</span>
        <span class="hidden w-20 shrink-0 text-right sm:block">Issues</span>
        <span class="hidden w-24 shrink-0 text-right md:block">Events 24h</span>
        <span class="hidden w-20 shrink-0 text-right lg:block">Total</span>
        <span class="hidden min-w-28 flex-[2] pl-6 lg:block">Issue activity</span>
        <span class="hidden w-28 shrink-0 text-right xl:block">Created</span>
        <span class="w-8" />
      </div>
      <div>
        {#each items as p (p.id)}
          {@const h = healthOf(p)}
          {@const d = p.stats ? deltaText(p.stats.events.current, p.stats.events.previous) : null}
          <div class="group flex items-center gap-4 border-b px-4 py-4 transition-colors last:border-b-0 hover:bg-muted/30">
            <Checkbox
              checked={selected.has(p.id)}
              onCheckedChange={(c: boolean | "indeterminate") => toggleOne(p.id, c === true)}
              aria-label={`Select ${p.name}`}
            />
            <span class="shrink-0"><PlatformIcon platform={p.platform ?? ""} size={28} /></span>
            <div class="min-w-0 flex-[3]">
              <button on:click={() => navigate(`/projects/${p.id}`)} class="block min-w-0 text-left transition-colors group-hover:text-primary">
                <div class="truncate text-base font-semibold">{p.name}</div>
                <div class="truncate font-mono text-xs text-muted-foreground">{p.slug}</div>
              </button>
            </div>
            <div class="hidden w-20 shrink-0 text-right sm:block">
              {#if p.stats}
                <div class={`text-sm font-medium tabular-nums ${p.stats.open_issues === 0 ? "text-muted-foreground" : ""}`}>
                  {compact(p.stats.open_issues)}
                </div>
                {#if p.stats.fatal_issues > 0}
                  <div class="text-xs font-medium tabular-nums text-red-600 dark:text-red-400">
                    {p.stats.fatal_issues} fatal
                  </div>
                {/if}
              {:else}<span class="text-sm text-muted-foreground/50">–</span>{/if}
            </div>
            <div class="hidden w-24 shrink-0 text-right md:block">
              {#if p.stats}
                <div class={`text-sm font-medium tabular-nums ${p.stats.events.current === 0 ? "text-muted-foreground" : ""}`} title={String(p.stats.events.current)}>
                  {compact(p.stats.events.current)}
                </div>
                {#if d}<div class={`text-xs tabular-nums ${d.bad ? "text-red-600 dark:text-red-400" : "text-muted-foreground"}`}>{d.text}</div>{/if}
              {:else}<span class="text-sm text-muted-foreground/50">–</span>{/if}
            </div>
            <div class="hidden w-20 shrink-0 text-right lg:block">
              {#if p.stats}
                <span class="text-sm tabular-nums text-muted-foreground" title={String(p.stats.total_events)}>{compact(p.stats.total_events)}</span>
              {:else}<span class="text-sm text-muted-foreground/50">–</span>{/if}
            </div>
            <div class="hidden min-w-28 flex-[2] pl-6 lg:block">
              {#if p.stats}<TrendSparkline trend={p.stats.trend} health={h} label={`Issue activity: ${healthLabel(h)}`} />{/if}
            </div>
            <div class="hidden w-28 shrink-0 text-right xl:block">
              <span class="text-sm text-muted-foreground">{relTime(p.createdAt)}</span>
            </div>
            <DropdownMenu.Root>
              <DropdownMenu.Trigger class="flex size-8 items-center justify-center rounded-md hover:bg-muted" aria-label={`Actions for ${p.name}`}>
                <MoreVertical class="size-4" />
              </DropdownMenu.Trigger>
              <DropdownMenu.Content align="end">
                <DropdownMenu.Item variant="destructive" onSelect={() => { pendingDelete = p; deleteOpen = true; }}>
                  <Trash2 class="mr-2 size-4" /> Delete
                </DropdownMenu.Item>
              </DropdownMenu.Content>
            </DropdownMenu.Root>
          </div>
        {/each}
      </div>
    </div>
  {/if}

  {#if !loading && !error}
    <TablePagination
      currentPage={page}
      {totalPages}
      {totalCount}
      perPage={PER_PAGE}
      disabled={busy}
      onPageChange={(p) => { page = p; load(); }}
    />
  {/if}

  {#if busy}
    <div class="pointer-events-none fixed inset-0 flex items-center justify-center bg-background/50">
      <LoaderCircle class="size-8 animate-spin text-primary" />
    </div>
  {/if}

  <DeleteProjectsDialog
    open={deleteOpen}
    count={selected.size}
    targetName={pendingDelete?.name}
    busy={busy}
    onConfirm={confirmDelete}
    onCancel={() => { deleteOpen = false; pendingDelete = null; }}
  />
</div>
