<script lang="ts">
  import { onMount } from "svelte";
  import { api, type IssueDetail, type IssueLink } from "$lib/api";
  import { loadPersisted, savePersisted, isString } from "$lib/persisted";
  import * as CardUI from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import { Root as Skeleton } from "$lib/components/ui/skeleton/index.js";
  import Input from "$lib/components/input.svelte";
  import LevelBadge from "$lib/components/LevelBadge.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import Button from "$lib/components/button.svelte";
  import Dialog from "$lib/components/Dialog.svelte";
  import { Copy, Check, CalendarClock, EyeOff, BellOff, Bell, Undo2, ExternalLink, TriangleAlert } from "lucide-svelte";
  import ConfirmDialog from "$lib/components/ConfirmDialog.svelte";
  import DateTime from "$lib/components/DateTime.svelte";
  import { toast } from "svelte-sonner";

  export let id: string;
  export let navigate: (path: string) => void;

  let data: IssueDetail | null = null;
  let error = "";
  let muteFor = loadPersisted<string>("splat.issue.muteFor", "forever", isString);
  $: savePersisted("splat.issue.muteFor", muteFor);
  let pendingAction: "resolved" | "resolved-next" | "ignored" | "muted" | "unresolved" | null = null;
  let actionBusy = false;
  let selectedEvent: string | null = null;
  let showRaw = false;
  let copiedRaw = false;
  let copiedRawTimer: ReturnType<typeof setTimeout>;
  let noteBody = "";
  let noteBusy = false;
  let confirmDeleteNote: number | null = null;
  let localSpans: number | null = null;
  let links: IssueLink[] = [];
  let showFileIssue = false;
  let fileTitle = "";
  let fileBody = "";
  let fileBusy = false;
  let confirmUnlink: IssueLink | null = null;
  let showStacktrace = true;
  let showEvents = true;
  let showNotes = true;
  let showLinks = true;

  async function load() {
    try {
      data = await api.issue(id, selectedEvent ?? undefined);
      links = data.links ?? [];
      localSpans = null;
      if (data.traceId) {
        try {
          const t = await api.projectTrace(data.project.id, data.traceId);
          localSpans = t.spans.length;
        } catch {
          localSpans = null;
        }
      }
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load issue.";
    }
  }

  function openFileDialog() {
    if (!data) return;
    fileTitle = data.issue.title;
    fileBody = "";
    showFileIssue = true;
  }

  async function fileIssue(e: Event) {
    e.preventDefault();
    if (!fileTitle.trim()) return;
    fileBusy = true;
    try {
      const link = await api.createIssueLink(id, { title: fileTitle.trim(), body: fileBody });
      links = [...links, link];
      showFileIssue = false;
      toast.success("External issue filed.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to file issue.");
    } finally {
      fileBusy = false;
    }
  }

  async function unlinkIssue() {
    if (!confirmUnlink) return;
    try {
      await api.deleteIssueLink(id, confirmUnlink.id);
      links = links.filter((l) => l.id !== confirmUnlink?.id);
      toast.success("External issue unlinked.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to unlink issue.");
    } finally {
      confirmUnlink = null;
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
    copiedRaw = true;
    clearTimeout(copiedRawTimer);
    copiedRawTimer = setTimeout(() => (copiedRaw = false), 2000);
  }

  async function addNote(e: Event) {
    e.preventDefault();
    if (!noteBody.trim() || !data) return;
    noteBusy = true;
    try {
      await api.addIssueNote(id, noteBody.trim());
      noteBody = "";
      toast.success("Note added.");
      await load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to add note.");
    } finally {
      noteBusy = false;
    }
  }

  async function deleteNote() {
    if (confirmDeleteNote === null || !data) return;
    try {
      await api.deleteIssueNote(id, confirmDeleteNote);
      data = { ...data, notes: data.notes.filter((n) => n.id !== confirmDeleteNote) };
      toast.success("Note deleted.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete note.");
    } finally {
      confirmDeleteNote = null;
    }
  }

  async function confirmAction() {
    if (!pendingAction) return;
    const doneMessages: Record<string, string> = {
      resolved: "Issue resolved.",
      "resolved-next": "Resolved in next release.",
      ignored: "Issue ignored.",
      muted: "Issue muted.",
      unresolved: "Issue reopened.",
    };
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
      toast.success(doneMessages[pendingAction]);
      pendingAction = null;
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update issue.");
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

  $: tracePath =
    data && data.traceId
      ? `/projects/${data.project.id}/traces/${encodeURIComponent(data.traceId)}`
      : "";
  $: projectPath = data ? `/projects/${data.project.id}` : "";

  function backToIssues() {
    try {
      const last = sessionStorage.getItem("splat.issues.last");
      if (last && last.startsWith("/issues")) {
        navigate(last);
        return;
      }
    } catch {
      // Storage unavailable — fall through to the plain list.
    }
    navigate("/issues");
  }

  function copyLink() {
    navigator.clipboard.writeText(window.location.href);
    toast.success("Link copied to clipboard.");
  }</script>

{#if error}
  <Alert.Root variant="destructive">
    <TriangleAlert />
    <Alert.Title>Couldn't load the issue</Alert.Title>
    <Alert.Description>{error}</Alert.Description>
  </Alert.Root>
{:else if !data}
  <div class="space-y-4" aria-label="Loading issue">
    <Skeleton class="h-6 w-2/3" />
    <Skeleton class="h-4 w-1/3" />
    <Skeleton class="h-48 w-full" />
    <Skeleton class="h-32 w-full" />
  </div>
{:else}
  <div class="space-y-6">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <div class="flex flex-wrap items-center gap-2">
          <LevelBadge level={data.issue.level} />
          <StatusBadge status={data.issue.status} />
          {#if data.issue.reopenedCount > 0}<span class="text-xs text-muted-foreground" title="Times a new occurrence flipped this issue back to unresolved">reopened {data.issue.reopenedCount}×</span>{/if}
          {#if data.issue.status === "muted"}<span class="text-xs text-muted-foreground">{formatMutedUntil(data.issue.mutedUntil)}</span>{/if}
          {#if data.issue.status === "resolved" && data.issue.resolvedInRelease}<span class="text-xs text-muted-foreground" title="Reopens automatically if the bug appears in a different release">pinned to {data.issue.resolvedInRelease}</span>{/if}
          <span class="text-xs text-muted-foreground"><button class="text-primary hover:underline" on:click={() => projectPath && navigate(projectPath)}>{data.project.name}</button> · {data.issue.environment} · {data.issue.eventCount} events</span>
        </div>
        <h2 class="mt-2 text-xl font-semibold tracking-tight">{data.issue.title}</h2>
        <p class="mt-1 font-mono text-xs text-muted-foreground">{data.issue.exceptionType}: {data.issue.exceptionValue}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <Button size="sm" variant="outline" title="Copy link to this issue" on:click={copyLink}><Copy class="h-3.5 w-3.5" /> Copy link</Button>
        <Button size="sm" variant="outline" class="border-green-600/50 text-green-700 hover:bg-green-50 dark:border-green-500/40 dark:text-green-400 dark:hover:bg-green-950" on:click={() => (pendingAction = "resolved")}><Check class="h-3.5 w-3.5" /> Resolve</Button>
        <Button size="sm" variant="outline" class="border-green-600/50 text-green-700 hover:bg-green-50 dark:border-green-500/40 dark:text-green-400 dark:hover:bg-green-950" title="Resolve, but reopen automatically if the bug appears in a different release" on:click={() => (pendingAction = "resolved-next")}><CalendarClock class="h-3.5 w-3.5" /> Resolve in next release</Button>
        <Button size="sm" variant="outline" class="text-muted-foreground" on:click={() => (pendingAction = "ignored")}><EyeOff class="h-3.5 w-3.5" /> Ignore</Button>
        {#if data.issue.status === "muted"}
          <Button size="sm" variant="outline" class="border-amber-600/50 text-amber-700 hover:bg-amber-50 dark:border-amber-500/40 dark:text-amber-400 dark:hover:bg-amber-950" on:click={() => (pendingAction = "unresolved")}><Bell class="h-3.5 w-3.5" /> Unmute</Button>
        {:else}
          <Button size="sm" variant="outline" class="border-amber-600/50 text-amber-700 hover:bg-amber-50 dark:border-amber-500/40 dark:text-amber-400 dark:hover:bg-amber-950" on:click={() => (pendingAction = "muted")}><BellOff class="h-3.5 w-3.5" /> Mute</Button>
        {/if}
        <Button size="sm" variant="outline" class="border-blue-600/50 text-blue-700 hover:bg-blue-50 dark:border-blue-500/40 dark:text-blue-400 dark:hover:bg-blue-950" on:click={() => (pendingAction = "unresolved")}><Undo2 class="h-3.5 w-3.5" /> Reopen</Button>
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
      <CardUI.Root class="gap-0">
        <CardUI.Content class="flex flex-wrap items-center gap-2 pt-6 text-sm">
          <span class="text-muted-foreground">trace_id</span>
          <code class="font-mono text-xs">{data.traceId}</code>
          {#if localSpans !== null && tracePath}
            <Button size="sm" variant="outline" on:click={() => navigate(tracePath)}>View trace ({localSpans} spans)</Button>
          {/if}
          {#if data.traceURL}<a class="text-primary hover:underline" href={data.traceURL} target="_blank" rel="noreferrer">Open in tracing backend</a>{/if}
        </CardUI.Content>
      </CardUI.Root>
    {/if}

    <nav aria-label="Issue sections" class="flex flex-wrap gap-x-4 gap-y-1 text-xs">
      {#if data.stacktrace.length}<a class="text-primary hover:underline" href="#section-stacktrace">Stacktrace</a>{/if}
      {#if data.events.length > 1}<a class="text-primary hover:underline" href="#section-events">Events ({data.events.length})</a>{/if}
      <a class="text-primary hover:underline" href="#section-notes">Notes ({data.notes.length})</a>
      <a class="text-primary hover:underline" href="#section-links">External issues ({links.length})</a>
    </nav>

    {#if data.stacktrace.length}
      <div id="section-stacktrace" class="scroll-mt-4">
      <CardUI.Root class="gap-0">
        <CardUI.Header class="flex-row items-center justify-between">
          <button class="text-sm font-medium hover:underline" on:click={() => (showStacktrace = !showStacktrace)}>{showStacktrace ? "▾" : "▸"} Stacktrace</button>
        </CardUI.Header>
        {#if showStacktrace}
        <CardUI.Content>
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
        </CardUI.Content>
        {/if}
      </CardUI.Root>
      </div>
    {/if}

    <div class="grid gap-4 lg:grid-cols-2">
      {#if data.tags.length}
        <CardUI.Root class="gap-0">
          <CardUI.Header><CardUI.Title>Tags</CardUI.Title></CardUI.Header>
          <CardUI.Content>
            {#each data.tags as t}<p class="flex justify-between gap-2 font-mono text-xs"><span class="text-muted-foreground">{t.key}</span><span class="truncate">{t.value}</span></p>{/each}
          </CardUI.Content>
        </CardUI.Root>
      {/if}
      {#if data.contexts.length}
        <CardUI.Root class="gap-0">
          <CardUI.Header><CardUI.Title>Contexts</CardUI.Title></CardUI.Header>
          <CardUI.Content>
            {#each data.contexts as c}
              <p class="mt-2 text-xs font-semibold first:mt-0">{c.name}</p>
              {#each c.fields as f}<p class="flex justify-between gap-2 font-mono text-xs"><span class="text-muted-foreground">{f.key}</span><span class="truncate">{f.value}</span></p>{/each}
            {/each}
          </CardUI.Content>
        </CardUI.Root>
      {/if}
      {#if data.breadcrumbs.length}
        <CardUI.Root class="gap-0">
          <CardUI.Header><CardUI.Title>Breadcrumbs</CardUI.Title></CardUI.Header>
          <CardUI.Content>
            {#each data.breadcrumbs as b}<p class="font-mono text-xs">[{b.timestamp}] {b.type}/{b.level}: {b.message}</p>{/each}
          </CardUI.Content>
        </CardUI.Root>
      {/if}
      {#if data.user}
        <CardUI.Root class="gap-0">
          <CardUI.Header><CardUI.Title>User</CardUI.Title></CardUI.Header>
          <CardUI.Content>
            <p class="font-mono text-xs">{data.user.username}</p>
            {#each data.user.extra as e}<p class="flex justify-between gap-2 font-mono text-xs"><span class="text-muted-foreground">{e.key}</span><span class="truncate">{e.value}</span></p>{/each}
          </CardUI.Content>
        </CardUI.Root>
      {/if}
    </div>

    {#if data.events.length > 1}
      {@const prevId = data.prevEventId}
      {@const nextId = data.nextEventId}
      <div id="section-events" class="scroll-mt-4">
      <CardUI.Root class="gap-0">
        <CardUI.Header class="flex-row items-center justify-between">
          <button class="text-sm font-medium hover:underline" on:click={() => (showEvents = !showEvents)}>{showEvents ? "▾" : "▸"} Events ({data.events.length})</button>
          <div class="flex items-center gap-1 text-xs">
            <Button size="sm" variant="outline" disabled={!prevId} on:click={() => prevId && showEvent(prevId)}>← Older</Button>
            <Button size="sm" variant="outline" disabled={!nextId} on:click={() => nextId && showEvent(nextId)}>Newer →</Button>
          </div>
        </CardUI.Header>
        {#if showEvents}
        <CardUI.Content>
          {#if data.currentEventId}
            <p class="mb-2 font-mono text-xs text-muted-foreground">Showing event <span class="text-foreground">{data.currentEventId}</span>{data.latestEventId && data.currentEventId !== data.latestEventId ? " (not the latest)" : ""}</p>
          {/if}
          <div class="max-h-64 space-y-1 overflow-y-auto">
            {#each data.events as e}
              <button class="block w-full truncate text-left font-mono text-xs hover:underline {e.eventId === data.currentEventId ? 'text-foreground' : 'text-muted-foreground'}" on:click={() => showEvent(e.eventId)}><DateTime iso={e.timestamp} /> — {e.eventId}</button>
            {/each}
          </div>
        </CardUI.Content>
        {/if}
      </CardUI.Root>
      </div>
    {/if}

    {#if data.rawEvent}
      <CardUI.Root class="gap-0">
        <CardUI.Header class="flex-row items-center justify-between">
          <button class="text-sm font-medium hover:underline" on:click={() => (showRaw = !showRaw)}>{showRaw ? "▾" : "▸"} Raw event JSON</button>
          {#if showRaw}
            <span class="flex items-center gap-2">
              <span class="inline-flex items-center gap-1 text-xs text-green-600 dark:text-green-400" aria-live="polite">
                {#if copiedRaw}<Check class="h-3 w-3" /> Copied{/if}
              </span>
              <Button size="sm" variant="outline" on:click={copyRaw}><Copy class="h-3.5 w-3.5" /> Copy</Button>
            </span>
          {/if}
        </CardUI.Header>
        {#if showRaw}
          <CardUI.Content>
            <pre class="max-h-96 overflow-auto rounded-md bg-muted p-3 font-mono text-xs">{JSON.stringify(data.rawEvent, null, 2)}</pre>
          </CardUI.Content>
        {/if}
      </CardUI.Root>
    {/if}

    <div id="section-notes" class="scroll-mt-4">
    <CardUI.Root class="gap-0">
      <CardUI.Header class="flex-row items-center justify-between">
        <button class="text-sm font-medium hover:underline" on:click={() => (showNotes = !showNotes)}>{showNotes ? "▾" : "▸"} Notes ({data.notes.length})</button>
      </CardUI.Header>
      {#if showNotes}
      <CardUI.Content>
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
      </CardUI.Content>
      {/if}
    </CardUI.Root>
    </div>

    <ConfirmDialog
      open={confirmDeleteNote !== null}
      title="Delete note?"
      description="The note will be permanently removed."
      confirmLabel="Delete"
      onConfirm={deleteNote}
      onCancel={() => (confirmDeleteNote = null)}
    />

    <div id="section-links" class="scroll-mt-4">
    <CardUI.Root class="gap-0">
      <CardUI.Header class="flex-row items-center justify-between">
        <button class="text-sm font-medium hover:underline" on:click={() => (showLinks = !showLinks)}>{showLinks ? "▾" : "▸"} External issues ({links.length})</button>
        <Button size="sm" variant="outline" on:click={openFileDialog}><ExternalLink class="h-3.5 w-3.5" /> File issue</Button>
      </CardUI.Header>
      {#if showLinks}
      <CardUI.Content>
        {#if links.length}
          <div class="mb-1 divide-y divide-border rounded-md border border-border">
            {#each links as l}
              <div class="flex flex-wrap items-center justify-between gap-2 p-2 text-xs">
                <span class="min-w-0">
                  <a class="font-medium text-primary hover:underline" href={l.url} target="_blank" rel="noreferrer">{l.repo}#{l.number}</a>
                  <span class="text-muted-foreground"> · {l.provider}{l.title ? ` · ${l.title}` : ""}</span>
                </span>
                <Button size="sm" variant="ghost" on:click={() => (confirmUnlink = l)}>Unlink</Button>
              </div>
            {/each}
          </div>
          <p class="text-xs text-muted-foreground">Unlinking only removes the reference here — the forge issue itself is untouched.</p>
        {:else}
          <p class="text-xs text-muted-foreground">Not filed anywhere yet. Needs a repository linked on the project page first.</p>
        {/if}
      </CardUI.Content>
      {/if}
    </CardUI.Root>
    </div>

    {#if showFileIssue}
      <Dialog open={true} title="File as external issue" onClose={() => (showFileIssue = false)}>
        <form on:submit={fileIssue} class="space-y-3">
          <div class="space-y-1"><label class="text-xs">Title</label><Input bind:value={fileTitle} /></div>
          <div class="space-y-1"><label class="text-xs">Body (blank = summary + backlink)</label><textarea rows="6" bind:value={fileBody} placeholder="Leave blank for the default summary." class="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" /></div>
          <div class="flex gap-2">
            <Button type="submit" size="sm" disabled={fileBusy || !fileTitle.trim()}>{fileBusy ? "Filing…" : "Create issue"}</Button>
            <Button size="sm" variant="outline" on:click={() => (showFileIssue = false)}>Cancel</Button>
          </div>
        </form>
      </Dialog>
    {/if}

    <ConfirmDialog
      open={confirmUnlink !== null}
      title="Unlink external issue?"
      description={confirmUnlink ? `“${confirmUnlink.repo}#${confirmUnlink.number}” stays on the forge — only the reference here is removed.` : ""}
      confirmLabel="Unlink"
      onConfirm={unlinkIssue}
      onCancel={() => (confirmUnlink = null)}
    />

    <Button variant="ghost" size="sm" on:click={backToIssues}>← Back to issues</Button>
  </div>
{/if}
