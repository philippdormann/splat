<script lang="ts">
  import { onMount } from "svelte";
  import { api, type ProjectDetail, type SourceMapMeta, type ProjectAccess, type User, type RepoConfig } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import { toast } from "svelte-sonner";
  import Badge from "$lib/components/badge.svelte";
  import ConfirmDialog from "$lib/components/ConfirmDialog.svelte";
  import Dialog from "$lib/components/Dialog.svelte";
  import PlatformIcon from "$lib/components/PlatformIcon.svelte";
  import ProjectOverview from "$lib/pages/ProjectOverview.svelte";
  import ProjectLogs from "$lib/pages/ProjectLogs.svelte";
  import ProjectTraces from "$lib/pages/ProjectTraces.svelte";
  import { ArrowLeft, Copy, RefreshCw, Trash2, Upload, Check, Rocket, FlaskConical, LayoutDashboard, Terminal, SlidersHorizontal, Users, ScrollText, Activity, GitBranch } from "lucide-svelte";

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
  let showGrant = false;
  let confirmRevoke: { id: number; username: string } | null = null;
  let repo: RepoConfig | null = null;
  let repoForm = { provider: "github", baseUrl: "", owner: "", repo: "", token: "" };
  let repoBusy = false;
  let confirmUnlinkRepo = false;

  const ptabs = [
    { id: "overview", label: "Overview", icon: LayoutDashboard },
    { id: "setup", label: "Setup", icon: Terminal },
    { id: "settings", label: "Settings", icon: SlidersHorizontal },
    { id: "sourcemaps", label: "Source maps", icon: Upload },
    { id: "logs", label: "Logs", icon: ScrollText },
    { id: "traces", label: "Traces", icon: Activity },
    { id: "repository", label: "Repository", icon: GitBranch },
    { id: "access", label: "Access", icon: Users, adminOnly: true },
  ];
  let ptab = "overview";
  $: visiblePtabs = ptabs.filter((t) => !t.adminOnly || isAdmin);
  $: if (!visiblePtabs.some((t) => t.id === ptab)) ptab = "overview";

  async function loadAccess() {
    try {
      const me = await api.me();
      isAdmin = me.role === "admin";
      repo = await api.projectRepo(id);
      if (repo) {
        repoForm = { provider: repo.provider, baseUrl: repo.baseUrl, owner: repo.owner, repo: repo.repo, token: "" };
      }
      if (isAdmin) {
        const [a, u] = await Promise.all([api.projectAccess(id), api.users()]);
        access = a;
        accessUsers = u;
      }
    } catch {
      // Non-admins get 401 here; the card simply stays hidden.
    }
  }

  async function saveRepo(e: Event) {
    e.preventDefault();
    repoBusy = true;
    try {
      repo = await api.saveProjectRepo(id, repoForm);
      repoForm = { ...repoForm, token: "" };
      toast.success("Repository linked.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not save repository.");
    } finally {
      repoBusy = false;
    }
  }

  async function testRepo() {
    repoBusy = true;
    try {
      const r = await api.testProjectRepo(id);
      if (r.ok) toast.success(r.message);
      else toast.error(r.message);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Connection test failed.");
    } finally {
      repoBusy = false;
    }
  }

  async function unlinkRepo() {
    try {
      await api.deleteProjectRepo(id);
      repo = null;
      repoForm = { provider: "github", baseUrl: "", owner: "", repo: "", token: "" };
      toast.success("Repository unlinked — existing issue links are kept.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not unlink repository.");
    } finally {
      confirmUnlinkRepo = false;
    }
  }

  async function grantAccess() {
    if (!grantUserId) return;
    try {
      await api.grantProjectMember(id, grantUserId);
      grantUserId = null;
      showGrant = false;
      access = await api.projectAccess(id);
      toast.success("Access granted.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not grant access.");
    }
  }

  async function revokeAccess() {
    if (!confirmRevoke) return;
    try {
      await api.revokeProjectMember(id, confirmRevoke.id);
      access = await api.projectAccess(id);
      toast.success("Access revoked.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not revoke access.");
    } finally {
      confirmRevoke = null;
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

    <div class="flex flex-col gap-4 md:flex-row">
      <nav aria-label="Project sections" class="flex shrink-0 gap-1 overflow-x-auto md:w-48 md:flex-col">
        {#each visiblePtabs as t}
          <button
            class={"flex items-center gap-2 whitespace-nowrap rounded-md px-3 py-2 text-sm font-medium transition-colors " + (ptab === t.id ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground")}
            on:click={() => (ptab = t.id)}
          >
            <svelte:component this={t.icon} class="h-4 w-4" />
            {t.label}
          </button>
        {/each}
      </nav>
      <div class="min-w-0 flex-1 space-y-4">
        {#if ptab === "overview"}
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
        {:else if ptab === "setup"}
    <Card cls="p-4">
      <p class="mb-2 text-sm font-medium">Setup — {data.platformLabel}</p>
      <p class="text-xs text-muted-foreground">{data.platformInstall}</p>
      <pre class="mt-2 overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs">{data.platformSnippet}</pre>
    </Card>
        {:else if ptab === "settings"}
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
        {:else if ptab === "sourcemaps"}
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
        {:else if ptab === "logs"}
    <ProjectLogs projectId={id} {navigate} />
        {:else if ptab === "traces"}
    <ProjectTraces projectId={id} {navigate} />
        {:else if ptab === "repository"}
    <Card cls="p-4">
      <div class="space-y-3">
        <p class="text-sm font-medium">Repository</p>
        <p class="text-xs text-muted-foreground">Link the codebase so issues can be filed as GitHub, GitLab, Gitea, or Forgejo issues. GitHub and GitLab work out of the box; Gitea/Forgejo need the instance base URL.</p>
        {#if isAdmin}
          <form on:submit={saveRepo} class="space-y-3">
            <div class="grid grid-cols-2 gap-3">
              <div class="space-y-1"><label class="text-xs">Provider</label>
                <select class="h-9 w-full rounded-md border border-input bg-background px-2 text-sm" bind:value={repoForm.provider}>
                  <option value="github">GitHub</option>
                  <option value="gitlab">GitLab</option>
                  <option value="gitea">Gitea</option>
                  <option value="forgejo">Forgejo</option>
                </select>
              </div>
              <div class="space-y-1"><label class="text-xs">Base URL (blank = provider default)</label><Input bind:value={repoForm.baseUrl} placeholder="https://git.example.com" autocomplete="off" /></div>
              <div class="space-y-1"><label class="text-xs">Owner / org</label><Input bind:value={repoForm.owner} placeholder="acme" autocomplete="off" /></div>
              <div class="space-y-1"><label class="text-xs">Repository</label><Input bind:value={repoForm.repo} placeholder="webapp" autocomplete="off" /></div>
              <div class="space-y-1"><label class="text-xs">API token {repo?.hasToken ? "(set — blank keeps it)" : ""}</label><Input type="password" bind:value={repoForm.token} placeholder={repo?.hasToken ? "leave blank to keep" : "token with issue write scope"} autocomplete="off" /></div>
            </div>
            <div class="flex gap-2">
              <Button type="submit" size="sm" disabled={repoBusy}>Save</Button>
              {#if repo}
                <Button size="sm" variant="outline" disabled={repoBusy} on:click={testRepo}>Test connection</Button>
                <Button size="sm" variant="outline" on:click={() => (confirmUnlinkRepo = true)}>Unlink</Button>
              {/if}
            </div>
          </form>
          {#if repo}
            <p class="font-mono text-xs text-muted-foreground">Linked: {repo.provider} · {repo.slug} · {repo.effectiveBaseUrl}</p>
          {/if}
          <ConfirmDialog
            open={confirmUnlinkRepo}
            title="Unlink repository?"
            description="Existing external-issue links stay on their issues; new issues just can't be filed until you link again."
            confirmLabel="Unlink"
            onConfirm={unlinkRepo}
            onCancel={() => (confirmUnlinkRepo = false)}
          />
        {:else if repo}
          <p class="font-mono text-xs text-muted-foreground">Linked: {repo.provider} · {repo.slug}</p>
        {:else}
          <p class="text-xs text-muted-foreground">No repository linked. An admin can link one here.</p>
        {/if}
      </div>
    </Card>
        {:else if ptab === "access"}
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
                  <button class="text-muted-foreground hover:text-foreground" aria-label="Revoke {accessUsername(uid)}" on:click={() => (confirmRevoke = { id: uid, username: accessUsername(uid) })}>✕</button>
                </span>
              {:else}
                <span class="text-xs text-muted-foreground">No direct grants.</span>
              {/each}
            </div>
            <div class="mt-2">
              <Button size="sm" variant="outline" on:click={() => (showGrant = true)}>Grant user</Button>
            </div>
            {#if showGrant}
              <Dialog open={true} title="Grant project access" onClose={() => (showGrant = false)}>
                <div class="space-y-3">
                  <p class="text-xs text-muted-foreground">The user sees this project's dashboard, issues, and events. Admins already have access everywhere; team grants are managed under Settings → Users & Teams.</p>
                  <div class="space-y-1">
                    <label class="text-xs">User</label>
                    <select class="h-9 w-full rounded-md border border-input bg-background px-2 text-sm" bind:value={grantUserId}>
                      <option value={null}>Pick a user…</option>
                      {#each accessUsers.filter((u) => u.role !== "admin" && !(access?.memberIds ?? []).includes(u.id)) as u}<option value={u.id}>{u.username}</option>{/each}
                    </select>
                  </div>
                  <div class="flex gap-2">
                    <Button size="sm" on:click={grantAccess} disabled={!grantUserId}>Grant access</Button>
                    <Button size="sm" variant="outline" on:click={() => (showGrant = false)}>Cancel</Button>
                  </div>
                </div>
              </Dialog>
            {/if}
            <ConfirmDialog
              open={confirmRevoke !== null}
              title={confirmRevoke ? `Revoke ${confirmRevoke.username}'s access?` : ""}
              description={`“${confirmRevoke?.username}” immediately loses this project's dashboard, issues, and events. Team grants are unaffected.`}
              confirmLabel="Revoke"
              onConfirm={revokeAccess}
              onCancel={() => (confirmRevoke = null)}
            />
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
        {/if}
      </div>
    </div>
  </div>
{/if}
