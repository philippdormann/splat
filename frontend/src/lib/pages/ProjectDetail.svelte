<script lang="ts">
  import { onMount } from "svelte";
  import { api, type ProjectDetail, type SourceMapMeta, type ProjectAccess, type User } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import { toast } from "svelte-sonner";
  import Badge from "$lib/components/badge.svelte";
  import ConfirmDialog from "$lib/components/ConfirmDialog.svelte";
  import PlatformIcon from "$lib/components/PlatformIcon.svelte";
  import ProjectOverview from "$lib/pages/ProjectOverview.svelte";
  import { ArrowLeft, Copy, RefreshCw, Trash2, Upload, Check, Rocket, FlaskConical } from "lucide-svelte";

  export let id: string;
  export let navigate: (path: string) => void;

  let data: ProjectDetail | null = null;
  let error = "";
  let maps: SourceMapMeta[] = [];
  let mapRelease = "";
  let mapBundleUrl = "";
  let mapBusy = false;
  let rotated = false;
  let rotatedTimer: ReturnType<typeof setTimeout>;
  let confirmDeleteProject = false;
  let confirmDeleteMap: number | null = null;
  let confirmRotate = false;
  let rotateBusy = false;
  let deleteBusy = false;
  let testBusy = false;
  let isAdmin = false;
  let access: ProjectAccess | null = null;
  let accessUsers: User[] = [];
  let grantUserId: number | null = null;

  async function loadAccess() {
    try {
      const me = await api.me();
      isAdmin = me.role === "admin";
      if (isAdmin) {
        const [a, u] = await Promise.all([api.projectAccess(id), api.users()]);
        access = a;
        accessUsers = u;
      }
    } catch {
      // Non-admins get 401 here; the card simply stays hidden.
    }
  }

  async function grantAccess() {
    if (!grantUserId) return;
    try {
      await api.grantProjectMember(id, grantUserId);
      grantUserId = null;
      access = await api.projectAccess(id);
      toast.success("Access granted.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not grant access.");
    }
  }

  async function revokeAccess(userId: number) {
    try {
      await api.revokeProjectMember(id, userId);
      access = await api.projectAccess(id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not revoke access.");
    }
  }

  function accessUsername(uid: number): string {
    return accessUsers.find((u) => u.id === uid)?.username ?? `User ${uid}`;
  }

  async function sendTestEvent() {
    testBusy = true;
    try {
      const r = await api.testEvent(id);
      toast.success("Test event ingested — opening its issue.");
      navigate(`/issues/${r.issueId}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Test event failed.");
    } finally {
      testBusy = false;
    }
  }

  async function load() {
    try {
      data = await api.project(id);
      maps = await api.sourceMaps(id);
      await loadAccess();
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load project.";
    }
  }

  async function uploadMap(e: Event) {
    e.preventDefault();
    const input = document.getElementById("sourcemap-file") as HTMLInputElement | null;
    const file = input?.files?.[0];
    if (!file) {
      toast.success("Choose a .map file first.");
      return;
    }
    if (!mapBundleUrl.trim()) {
      toast.success("Bundle URL is required (e.g. https://example.com/static/app.min.js).");
      return;
    }
    mapBusy = true;
    try {
      const content = await file.text();
      await api.uploadSourceMap(id, { release: mapRelease.trim(), bundleUrl: mapBundleUrl.trim(), content });
      maps = await api.sourceMaps(id);
      toast.success(`Source map uploaded for ${mapBundleUrl.trim()}.`);
      mapRelease = "";
      mapBundleUrl = "";
      if (input) input.value = "";
    } catch (err) {
      toast.error(`Upload failed: ${err instanceof Error ? err.message : err}`);
    } finally {
      mapBusy = false;
    }
  }

  async function deleteMap() {
    if (confirmDeleteMap === null) return;
    deleteBusy = true;
    try {
      await api.deleteSourceMap(id, confirmDeleteMap);
      maps = await api.sourceMaps(id);
      toast.success("Source map deleted.");
    } catch (e) {
      toast.error(`Delete failed: ${e instanceof Error ? e.message : e}`);
    } finally {
      deleteBusy = false;
      confirmDeleteMap = null;
    }
  }

  async function save(e: Event) {
    e.preventDefault();
    if (!data) return;
    const updated = await api.updateProject(id, {
      name: data.project.name,
      defaultEnvironment: data.project.defaultEnvironment,
      platform: data.project.platform,
      retentionDays: data.project.retentionDays,
      enabled: data.project.enabled,
    });
    data = updated;
    toast.success("Saved.");
  }

  async function rotate() {
    rotateBusy = true;
    try {
      const r = await api.rotateProjectKey(id);
      if (data) data = { ...data, dsn: r.dsn };
      rotated = true;
      clearTimeout(rotatedTimer);
      rotatedTimer = setTimeout(() => (rotated = false), 4000);
      toast.success("DSN rotated — update your apps to the new DSN.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Rotate failed.");
    } finally {
      rotateBusy = false;
      confirmRotate = false;
    }
  }

  async function remove() {
    deleteBusy = true;
    try {
      await api.deleteProject(id);
      navigate("/projects");
    } catch (e) {
      toast.error(`Delete failed: ${e instanceof Error ? e.message : e}`);
    } finally {
      deleteBusy = false;
      confirmDeleteProject = false;
    }
  }

  function copy(text: string) {
    navigator.clipboard.writeText(text);
    toast.success("Copied to clipboard.");
  }

  onMount(load);
</script>

{#if error}
  <p class="text-sm text-destructive">{error}</p>
{:else if !data}
  <div class="mx-auto w-full max-w-6xl space-y-4" aria-label="Loading project">
    <div class="h-8 w-1/3 animate-pulse rounded bg-muted"></div>
    <div class="h-24 animate-pulse rounded-lg bg-muted"></div>
    <div class="h-64 animate-pulse rounded-lg bg-muted"></div>
  </div>
{:else}
  <div class="mx-auto w-full max-w-6xl space-y-4">
    <button class="inline-flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground" on:click={() => navigate("/projects")}>
      <ArrowLeft class="h-4 w-4" /> Back to projects
    </button>
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex min-w-0 items-center gap-3">
        <PlatformIcon platform={data.project.platform} size={32} />
        <div class="min-w-0">
          <h2 class="truncate text-xl font-extrabold tracking-tight md:text-2xl">{data.project.name}</h2>
          <div class="mt-1 flex flex-wrap items-center gap-2">
            <Badge>{data.platformLabel}</Badge>
            <span class="font-mono text-xs text-muted-foreground">{data.project.slug} · {data.project.defaultEnvironment}</span>
            {#if !data.project.enabled}<Badge variant="secondary">disabled</Badge>{/if}
          </div>
        </div>
      </div>
      <Button size="sm" variant="outline" on:click={() => navigate(`/projects/${id}/releases`)}><Rocket class="h-4 w-4" /> Releases</Button>
      <Button size="sm" variant="outline" disabled={testBusy} on:click={sendTestEvent}><FlaskConical class="h-4 w-4" /> {testBusy ? "Sending…" : "Send test event"}</Button>
      <Button size="sm" variant="destructive" on:click={() => (confirmDeleteProject = true)}><Trash2 class="h-4 w-4" /> Delete</Button>
    </div>

    <ConfirmDialog
      open={confirmDeleteProject}
      title="Delete project?"
      description={`“${data.project.name}” and all its issues, events and source maps will be permanently deleted. This cannot be undone.`}
      confirmLabel="Delete project"
      busy={deleteBusy}
      onConfirm={remove}
      onCancel={() => { if (!deleteBusy) confirmDeleteProject = false; }}
    />

    <ConfirmDialog
      open={confirmDeleteMap !== null}
      title="Delete source map?"
      description="New events will show minified frames again."
      confirmLabel="Delete"
      busy={deleteBusy}
      onConfirm={deleteMap}
      onCancel={() => { if (!deleteBusy) confirmDeleteMap = null; }}
    />

    <ConfirmDialog
      open={confirmRotate}
      title="Rotate DSN key?"
      description="The current DSN stops working immediately. Update every app using it, or their events will be rejected."
      confirmLabel="Rotate key"
      busy={rotateBusy}
      onConfirm={rotate}
      onCancel={() => { if (!rotateBusy) confirmRotate = false; }}
    />

    <Card cls="space-y-2 p-4">
      <p class="text-sm font-medium">DSN
        {#if rotated}<span class="ml-2 inline-flex items-center gap-1 rounded-full bg-green-100 px-2 py-0.5 text-xs font-semibold text-green-700 dark:bg-green-900 dark:text-green-300"><Check class="h-3 w-3" /> Rotated</span>{/if}
      </p>
      <div class="flex gap-2">
        <code class="flex-1 overflow-x-auto rounded-md bg-muted p-2 font-mono text-xs transition-colors {rotated ? 'ring-2 ring-green-500' : ''}">{data.dsn}</code>
        <Button size="sm" variant="outline" on:click={() => data && copy(data.dsn)}><Copy class="h-4 w-4" /></Button>
        <Button size="sm" variant="outline" on:click={() => (confirmRotate = true)}><RefreshCw class="h-4 w-4" /> Rotate</Button>
      </div>
      <p class="text-xs text-muted-foreground">{data.stats.events7d} events / 7d · {data.stats.unresolved} unresolved</p>
    </Card>

    <ProjectOverview projectId={id} {navigate} />

    <Card cls="p-4">
      <p class="mb-2 text-sm font-medium">Setup — {data.platformLabel}</p>
      <p class="text-xs text-muted-foreground">{data.platformInstall}</p>
      <pre class="mt-2 overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs">{data.platformSnippet}</pre>
    </Card>

    <Card cls="p-4">
      <form on:submit={save} class="space-y-3">
        <p class="text-sm font-medium">Settings</p>
        <div class="grid grid-cols-2 gap-3">
          <div class="space-y-1"><label class="text-xs">Name</label><Input bind:value={data.project.name} /></div>
          <div class="space-y-1"><label class="text-xs">Default environment</label><Input bind:value={data.project.defaultEnvironment} /></div>
          <div class="space-y-1"><label class="text-xs">Retention (days, 0 = keep forever)</label><Input type="number" min="0" bind:value={data.project.retentionDays} /></div>
          <div class="space-y-1">
            <label class="text-xs">Platform</label>
            <select class="h-9 w-full rounded-md border border-input bg-background px-2 text-sm" bind:value={data.project.platform}>
              {#each data.platforms as p}<option value={p.id}>{p.label}</option>{/each}
            </select>
          </div>
        </div>
        <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={data.project.enabled} /> Enabled</label>
        <Button type="submit" size="sm">Save</Button>
      </form>
    </Card>

    <Card cls="p-4">
      <div class="space-y-3">
        <p class="text-sm font-medium">Source maps</p>
        <p class="text-xs text-muted-foreground">Upload a <span class="font-mono">.map</span> file per release + bundle URL and minified JS frames resolve to the original source. Re-uploading the same triple replaces it. Maps uploaded without a release act as a fallback.</p>
        <form on:submit={uploadMap} class="grid grid-cols-2 gap-3">
          <div class="space-y-1"><label class="text-xs">Release (optional)</label><Input bind:value={mapRelease} placeholder="1.2.3" /></div>
          <div class="space-y-1"><label class="text-xs">Bundle URL</label><Input bind:value={mapBundleUrl} placeholder="https://example.com/static/app.min.js" /></div>
          <div class="col-span-2"><input id="sourcemap-file" type="file" accept=".map,application/json" class="text-xs" /></div>
          <div class="col-span-2"><Button type="submit" size="sm" disabled={mapBusy}><Upload class="h-4 w-4" /> {mapBusy ? "Uploading…" : "Upload source map"}</Button></div>
        </form>
        {#if maps.length}
          <div class="divide-y divide-border rounded-md border border-border">
            {#each maps as m}
              <div class="flex items-center justify-between gap-2 p-2 font-mono text-xs">
                <span class="truncate">{m.bundleUrl} <span class="text-muted-foreground">· {m.release || "(any release)"} · {(m.sizeBytes / 1024).toFixed(1)} KB</span></span>
                <Button size="sm" variant="outline" on:click={() => (confirmDeleteMap = m.id)}>Delete</Button>
              </div>
            {/each}
          </div>
        {:else}
          <p class="text-xs text-muted-foreground">No source maps uploaded.</p>
        {/if}
      </div>
    </Card>

    {#if isAdmin && access}
      <Card cls="p-4">
        <div class="space-y-3">
          <p class="text-sm font-medium">Access</p>
          <p class="text-xs text-muted-foreground">Admins see everything. Members need a direct grant below or a team grant (manage teams under Settings → Users & Teams).</p>
          <div>
            <p class="text-xs text-muted-foreground">Direct members ({access.memberIds.length})</p>
            <div class="mt-1 flex flex-wrap gap-1">
              {#each access.memberIds as uid}
                <span class="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-xs">
                  {accessUsername(uid)}
                  <button class="text-muted-foreground hover:text-foreground" aria-label="Revoke {accessUsername(uid)}" on:click={() => revokeAccess(uid)}>✕</button>
                </span>
              {:else}
                <span class="text-xs text-muted-foreground">No direct grants.</span>
              {/each}
            </div>
            <div class="mt-2 flex gap-2">
              <select class="h-8 flex-1 rounded-md border border-input bg-background px-2 text-xs" bind:value={grantUserId}>
                <option value={null}>Grant user…</option>
                {#each accessUsers.filter((u) => !(access?.memberIds ?? []).includes(u.id)) as u}<option value={u.id}>{u.username} ({u.role})</option>{/each}
              </select>
              <Button size="sm" variant="outline" on:click={grantAccess}>Grant</Button>
            </div>
          </div>
          <div>
            <p class="text-xs text-muted-foreground">Teams ({access.teams.length})</p>
            <div class="mt-1 flex flex-wrap gap-1">
              {#each access.teams as t}
                <span class="rounded-md bg-muted px-2 py-0.5 text-xs">{t.name} <span class="font-mono text-muted-foreground">{t.slug}</span></span>
              {:else}
                <span class="text-xs text-muted-foreground">No team grants.</span>
              {/each}
            </div>
          </div>
        </div>
      </Card>
    {/if}
  </div>
{/if}
