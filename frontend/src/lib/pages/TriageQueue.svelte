<script lang="ts">
  import { onMount } from "svelte";
  import { api, type IssuesData, type IssueDetail } from "$lib/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import * as Empty from "$lib/components/ui/empty/index.js";
  import { Root as Skeleton } from "$lib/components/ui/skeleton/index.js";
  import Button from "$lib/components/button.svelte";
  import LevelBadge from "$lib/components/LevelBadge.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import RelativeTime from "$lib/components/RelativeTime.svelte";
  import DateTime from "$lib/components/DateTime.svelte";
  import { stashQueue } from "$lib/queue";
  import { toast } from "svelte-sonner";
  import { Check, EyeOff, BellOff, SkipForward, ExternalLink, ListChecks, TriangleAlert } from "lucide-svelte";

  export let navigate: (path: string) => void;

  type Row = IssuesData["issues"][number];

  let items: Row[] = [];
  let total = 0;
  let page = 1;
  let totalPages = 1;
  let idx = 0;
  let detail: IssueDetail | null = null;
  let loading = true;
  let error = "";
  let busy = false;
  let done = 0;

  async function loadPage(p: number) {
    const params = new URLSearchParams({ status: "unresolved", page: String(p) });
    const res = await api.issues(params);
    total = res.total;
    totalPages = res.totalPages;
    page = p;
    const known = new Set(items.map((i) => i.id));
    items = [...items, ...res.issues.filter((i) => !known.has(i.id))];
  }

  async function showCurrent() {
    if (idx >= items.length && page < totalPages) await loadPage(page + 1);
    if (idx < items.length) {
      detail = await api.issue(items[idx].id);
    } else {
      detail = null;
    }
  }

  async function act(status: "resolved" | "ignored" | "muted", label: string) {
    const cur = items[idx];
    if (!cur || busy) return;
    busy = true;
    try {
      await api.setIssueStatus(cur.id, status);
      toast.success(`${label} “${cur.title}”.`);
      done += 1;
      items = items.filter((i) => i.id !== cur.id);
      await showCurrent();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update issue.");
    } finally {
      busy = false;
    }
  }

  async function skip() {
    idx += 1;
    detail = null;
    try {
      await showCurrent();
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load issue.";
    }
  }

  function openFull() {
    const cur = items[idx];
    if (!cur) return;
    stashQueue(items.slice(idx).map((i) => i.id));
    navigate(`/issues/${cur.id}`);
  }

  onMount(async () => {
    try {
      await loadPage(1);
      await showCurrent();
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load queue.";
    } finally {
      loading = false;
    }
  });
</script>

<div class="mx-auto w-full max-w-3xl space-y-4">
  <div>
    <h1 class="text-2xl font-semibold tracking-tight">Triage queue</h1>
    <p class="text-muted-foreground mt-1 text-sm">
      {#if loading}Loading unresolved issues…{:else if detail}{done} handled this session · {total} unresolved at start{:else if done > 0}{done} handled — queue clear{:else}No unresolved issues{/if}
    </p>
  </div>

  {#if error}
    <Alert.Root variant="destructive">
      <TriangleAlert />
      <Alert.Title>Couldn't load the queue</Alert.Title>
      <Alert.Description>{error}</Alert.Description>
    </Alert.Root>
  {:else if loading}
    <Card.Root><Card.Content class="space-y-2 pt-6" aria-label="Loading queue"><Skeleton class="h-8 w-2/3" /><Skeleton class="h-40 w-full" /></Card.Content></Card.Root>
  {:else if !detail}
    <Empty.Root>
      <Empty.Header>
        <span class="bg-muted flex size-12 items-center justify-center rounded-xl">
          <ListChecks class="text-muted-foreground size-6" />
        </span>
        <Empty.Title>Queue clear</Empty.Title>
        <Empty.Description>{done > 0 ? `You triaged ${done} issue${done === 1 ? "" : "s"} this session. Enjoy the quiet.` : "No unresolved issues. Enjoy the quiet."}</Empty.Description>
      </Empty.Header>
      <Empty.Content>
        <Button on:click={() => navigate("/issues")}>Back to issues</Button>
      </Empty.Content>
    </Empty.Root>
  {:else}
    {@const cur = items[idx]}
    <Card.Root class="gap-0">
      <Card.Content class="space-y-3 pt-6">
        <div class="flex flex-wrap items-center gap-2">
          <LevelBadge level={detail.issue.level} />
          <StatusBadge status={detail.issue.status} />
          <span class="text-muted-foreground text-xs">{detail.project.name} · {detail.issue.environment} · {detail.issue.eventCount} events · open <RelativeTime iso={detail.issue.firstSeen} /></span>
        </div>
        <h2 class="text-lg font-semibold tracking-tight">{detail.issue.title}</h2>
        <p class="font-mono text-xs text-muted-foreground">{detail.issue.exceptionType}: {detail.issue.exceptionValue}</p>
        {#if detail.stacktrace.length}
          <div class="space-y-1 rounded-md bg-muted p-2 font-mono text-xs">
            {#each detail.stacktrace.slice(0, 6) as f}
              <p class="truncate">{f.module ? `${f.module}.` : ""}{f.function} — {f.file}:{f.line}</p>
            {/each}
            {#if detail.stacktrace.length > 6}<p class="text-muted-foreground">+ {detail.stacktrace.length - 6} more frames</p>{/if}
          </div>
        {/if}
        <p class="font-mono text-xs text-muted-foreground">Last seen <DateTime iso={detail.issue.lastSeen} /></p>
      </Card.Content>
    </Card.Root>

    <div class="flex flex-wrap items-center gap-2">
      <Button size="sm" class="bg-green-700 hover:bg-green-800 dark:bg-green-600 dark:hover:bg-green-700" disabled={busy} on:click={() => act("resolved", "Resolved")}><Check class="h-4 w-4" /> Resolve & next</Button>
      <Button size="sm" variant="outline" disabled={busy} on:click={() => act("ignored", "Ignored")}><EyeOff class="h-4 w-4" /> Ignore</Button>
      <Button size="sm" variant="outline" disabled={busy} title="Mute forever — open full detail for timed or count-based mutes" on:click={() => act("muted", "Muted")}><BellOff class="h-4 w-4" /> Mute</Button>
      <Button size="sm" variant="outline" disabled={busy} on:click={skip}><SkipForward class="h-4 w-4" /> Skip</Button>
      {#if cur}<Button size="sm" variant="ghost" on:click={openFull}><ExternalLink class="h-4 w-4" /> Full detail</Button>{/if}
    </div>
  {/if}
</div>
