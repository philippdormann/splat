<script lang="ts">
  import { onMount } from "svelte";
  import { api, type IssueDetail } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Input from "$lib/components/input.svelte";
  import LevelBadge from "$lib/components/LevelBadge.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import Button from "$lib/components/button.svelte";
  import ConfirmDialog from "$lib/components/ConfirmDialog.svelte";
  import DateTime from "$lib/components/DateTime.svelte";

  export let id: string;
  export let navigate: (path: string) => void;

  let data: IssueDetail | null = null;
  let error = "";
  let muteFor = "forever";
  let pendingAction: "resolved" | "resolved-next" | "ignored" | "muted" | "unresolved" | null = null;
  let actionBusy = false;
  let selectedEvent: string | null = null;
  let showRaw = false;
  let noteBody = "";
  let noteBusy = false;
  let confirmDeleteNote: number | null = null;

  async function load() {
    try {
      data = await api.issue(id, selectedEvent ?? undefined);
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load issue.";
    }
  }

  function showEvent(eventId: string) {
    selectedEvent = eventId;
    showRaw = false;
    load();
    window.scrollTo(0, 0);
  }

  function copyRaw() {
    if (!data?.rawEvent) return;
    navigator.clipboard.writeText(JSON.stringify(data.rawEvent, null, 2));
  }

  async function addNote(e: Event) {
    e.preventDefault();
    if (!noteBody.trim() || !data) return;
    noteBusy = true;
    try {
      await api.addIssueNote(id, noteBody.trim());
      noteBody = "";
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to add note.";
    } finally {
      noteBusy = false;
    }
  }

  async function deleteNote() {
    if (confirmDeleteNote === null || !data) return;
    try {
      await api.deleteIssueNote(id, confirmDeleteNote);
      data = { ...data, notes: data.notes.filter((n) => n.id !== confirmDeleteNote) };
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to delete note.";
    } finally {
      confirmDeleteNote = null;
    }
  }

  async function confirmAction() {
    if (!pendingAction) return;
    actionBusy = true;
    try {
      if (pendingAction === "muted") {
        let mutedUntil: string | undefined;
        if (muteFor !== "forever") {
          const hours = { "24h": 24, "7d": 24 * 7, "30d": 24 * 30 }[muteFor] ?? 0;
          if (hours > 0) mutedUntil = new Date(Date.now() + hours * 3600 * 1000).toISOString();
        }
        await api.setIssueStatus(id, "muted", mutedUntil);
      } else if (pendingAction === "resolved-next") {
        await api.setIssueStatus(id, "resolved", undefined, true);
      } else {
        await api.setIssueStatus(id, pendingAction);
      }
      pendingAction = null;
      await load();
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to update issue.";
      pendingAction = null;
    } finally {
      actionBusy = false;
    }
  }

  const actionCopy: Record<string, { title: string; description: string; label: string }> = {
    resolved: { title: "Resolve issue?", description: "New occurrences will reopen it.", label: "Resolve" },
    "resolved-next": { title: "Resolve in next release?", description: "Stays resolved for the current release and reopens automatically if the bug appears in a different release.", label: "Resolve in next release" },
    ignored: { title: "Ignore issue?", description: "The issue stays ignored permanently — new occurrences will not reopen it and it will not notify.", label: "Ignore" },
    muted: { title: "Mute issue?", description: "Muted issues keep counting events but never reopen and never notify. A timed mute lifts automatically.", label: "Mute" },
    unresolved: { title: "Reopen issue?", description: "The issue returns to unresolved and can notify again.", label: "Reopen" },
  };

  function formatMutedUntil(iso: string | null): string {
    if (!iso) return "muted indefinitely";
    const d = new Date(iso);
    return `muted until ${d.toLocaleString()}`;
  }

  onMount(load);
</script>

{#if error}
  <p class="text-sm text-destructive">{error}</p>
{:else if !data}
  <div class="space-y-4" aria-label="Loading issue">
    <div class="h-6 w-2/3 animate-pulse rounded bg-muted"></div>
    <div class="h-4 w-1/3 animate-pulse rounded bg-muted"></div>
    <div class="h-48 animate-pulse rounded-lg bg-muted"></div>
    <div class="h-32 animate-pulse rounded-lg bg-muted"></div>
  </div>
{:else}
  <div class="space-y-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <div class="flex flex-wrap items-center gap-2">
          <LevelBadge level={data.issue.level} />
          <StatusBadge status={data.issue.status} />
          {#if data.issue.status === "muted"}<span class="text-xs text-muted-foreground">{formatMutedUntil(data.issue.mutedUntil)}</span>{/if}
          {#if data.issue.status === "resolved" && data.issue.resolvedInRelease}<span class="text-xs text-muted-foreground" title="Reopens automatically if the bug appears in a different release">pinned to {data.issue.resolvedInRelease}</span>{/if}
          <span class="text-xs text-muted-foreground">{data.project.name} · {data.issue.environment} · {data.issue.eventCount} events</span>
        </div>
        <h2 class="mt-2 text-xl font-bold">{data.issue.title}</h2>
        <p class="mt-1 font-mono text-xs text-muted-foreground">{data.issue.exceptionType}: {data.issue.exceptionValue}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <Button size="sm" variant="outline" on:click={() => (pendingAction = "resolved")}>Resolve</Button>
        <Button size="sm" variant="outline" title="Resolve, but reopen automatically if the bug appears in a different release" on:click={() => (pendingAction = "resolved-next")}>Resolve in next release</Button>
        <Button size="sm" variant="outline" on:click={() => (pendingAction = "ignored")}>Ignore</Button>
        {#if data.issue.status === "muted"}
          <Button size="sm" variant="outline" on:click={() => (pendingAction = "unresolved")}>Unmute</Button>
        {:else}
          <Button size="sm" variant="outline" on:click={() => (pendingAction = "muted")}>Mute</Button>
        {/if}
        <Button size="sm" variant="outline" on:click={() => (pendingAction = "unresolved")}>Reopen</Button>
      </div>
    </div>

    <ConfirmDialog
      open={pendingAction !== null}
      title={pendingAction ? actionCopy[pendingAction].title : ""}
      description={pendingAction ? actionCopy[pendingAction].description : ""}
      confirmLabel={pendingAction ? actionCopy[pendingAction].label : ""}
      variant={pendingAction === "unresolved" ? "default" : "destructive"}
      busy={actionBusy}
      onConfirm={confirmAction}
      onCancel={() => { if (!actionBusy) pendingAction = null; }}
    >
      {#if pendingAction === "muted"}
        <span class="mt-3 block">
          <select class="h-8 rounded-md border border-input bg-background px-2 text-xs" bind:value={muteFor} aria-label="Mute duration">
            <option value="forever">Mute forever</option>
            <option value="24h">Mute 24 hours</option>
            <option value="7d">Mute 7 days</option>
            <option value="30d">Mute 30 days</option>
          </select>
        </span>
      {/if}
    </ConfirmDialog>

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
      {@const prevId = data.prevEventId}
      {@const nextId = data.nextEventId}
      <Card cls="p-4">
        <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
          <p class="text-sm font-medium">Events ({data.events.length})</p>
          <div class="flex items-center gap-1 text-xs">
            <Button size="sm" variant="outline" disabled={!prevId} on:click={() => prevId && showEvent(prevId)}>← Older</Button>
            <Button size="sm" variant="outline" disabled={!nextId} on:click={() => nextId && showEvent(nextId)}>Newer →</Button>
          </div>
        </div>
        {#if data.currentEventId}
          <p class="mb-2 font-mono text-xs text-muted-foreground">Showing event <span class="text-foreground">{data.currentEventId}</span>{data.latestEventId && data.currentEventId !== data.latestEventId ? " (not the latest)" : ""}</p>
        {/if}
        <div class="max-h-64 space-y-1 overflow-y-auto">
          {#each data.events as e}
            <button class="block w-full truncate text-left font-mono text-xs hover:underline {e.eventId === data.currentEventId ? 'text-foreground' : 'text-muted-foreground'}" on:click={() => showEvent(e.eventId)}><DateTime iso={e.timestamp} /> — {e.eventId}</button>
          {/each}
        </div>
      </Card>
    {/if}

    {#if data.rawEvent}
      <Card cls="p-4">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <button class="text-sm font-medium hover:underline" on:click={() => (showRaw = !showRaw)}>{showRaw ? "▾" : "▸"} Raw event JSON</button>
          {#if showRaw}<Button size="sm" variant="outline" on:click={copyRaw}>Copy</Button>{/if}
        </div>
        {#if showRaw}
          <pre class="mt-2 max-h-96 overflow-auto rounded-md bg-muted p-3 font-mono text-xs">{JSON.stringify(data.rawEvent, null, 2)}</pre>
        {/if}
      </Card>
    {/if}

    <Card cls="p-4">
      <p class="mb-2 text-sm font-medium">Notes ({data.notes.length})</p>
      {#if data.notes.length}
        <div class="mb-3 space-y-2">
          {#each data.notes as n}
            <div class="rounded-md bg-muted p-2">
              <p class="whitespace-pre-wrap text-sm">{n.body}</p>
              <div class="mt-1 flex items-center justify-between gap-2">
                <span class="font-mono text-xs text-muted-foreground"><DateTime iso={n.createdAt} /></span>
                <Button size="sm" variant="ghost" on:click={() => (confirmDeleteNote = n.id)}>Delete</Button>
              </div>
            </div>
          {/each}
        </div>
      {:else}
        <p class="mb-3 text-xs text-muted-foreground">No notes yet — jot down context for your future self.</p>
      {/if}
      <form on:submit={addNote} class="flex gap-2">
        <Input bind:value={noteBody} placeholder="Add a note…" disabled={noteBusy} class="flex-1" />
        <Button type="submit" size="sm" disabled={noteBusy || !noteBody.trim()}>Add</Button>
      </form>
    </Card>

    <ConfirmDialog
      open={confirmDeleteNote !== null}
      title="Delete note?"
      description="The note will be permanently removed."
      confirmLabel="Delete"
      onConfirm={deleteNote}
      onCancel={() => (confirmDeleteNote = null)}
    />

    <Button variant="ghost" size="sm" on:click={() => navigate("/issues")}>← Back to issues</Button>
  </div>
{/if}
