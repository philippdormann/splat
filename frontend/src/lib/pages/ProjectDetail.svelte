<script lang="ts">
  import { onMount } from "svelte";
  import { api, type ProjectDetail, type SourceMapMeta } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import { Copy, RefreshCw, Trash2, Upload, Check } from "lucide-svelte";

  export let id: string;
  export let navigate: (path: string) => void;

  let data: ProjectDetail | null = null;
  let error = "";
  let notice = "";
  let maps: SourceMapMeta[] = [];
  let mapRelease = "";
  let mapBundleUrl = "";
  let mapBusy = false;
  let rotated = false;
  let rotatedTimer: ReturnType<typeof setTimeout>;

  async function load() {
    try {
      data = await api.project(id);
      maps = await api.sourceMaps(id);
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load project.";
    }
  }

  async function uploadMap(e: Event) {
    e.preventDefault();
    const input = document.getElementById("sourcemap-file") as HTMLInputElement | null;
    const file = input?.files?.[0];
    if (!file) {
      notice = "Choose a .map file first.";
      return;
    }
    if (!mapBundleUrl.trim()) {
      notice = "Bundle URL is required (e.g. https://example.com/static/app.min.js).";
      return;
    }
    mapBusy = true;
    try {
      const content = await file.text();
      await api.uploadSourceMap(id, { release: mapRelease.trim(), bundleUrl: mapBundleUrl.trim(), content });
      maps = await api.sourceMaps(id);
      notice = `Source map uploaded for ${mapBundleUrl.trim()}.`;
      mapRelease = "";
      mapBundleUrl = "";
      if (input) input.value = "";
    } catch (err) {
      notice = `Upload failed: ${err instanceof Error ? err.message : err}`;
    } finally {
      mapBusy = false;
    }
  }

  async function deleteMap(mapId: number) {
    if (!confirm("Delete this source map? New events will show minified frames again.")) return;
    await api.deleteSourceMap(id, mapId);
    maps = await api.sourceMaps(id);
    notice = "Source map deleted.";
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
    notice = "Saved.";
  }

  async function rotate() {
    const r = await api.rotateProjectKey(id);
    if (data) data = { ...data, dsn: r.dsn };
    rotated = true;
    clearTimeout(rotatedTimer);
    rotatedTimer = setTimeout(() => (rotated = false), 4000);
  }

  async function remove() {
    if (!confirm("Delete this project and all its issues/events?")) return;
    await api.deleteProject(id);
    navigate("/projects");
  }

  function copy(text: string) {
    navigator.clipboard.writeText(text);
    notice = "Copied to clipboard.";
  }

  onMount(load);
</script>

{#if error}
  <p class="text-sm text-destructive">{error}</p>
{:else if !data}
  <p class="text-sm text-muted-foreground">Loading…</p>
{:else}
  <div class="mx-auto max-w-3xl space-y-4">
    <div class="flex items-center justify-between">
      <h2 class="text-2xl font-bold">{data.project.name}</h2>
      <Button size="sm" variant="destructive" on:click={remove}><Trash2 class="h-4 w-4" /> Delete</Button>
    </div>
    {#if notice}<p class="text-sm text-muted-foreground">{notice}</p>{/if}

    <Card cls="space-y-2 p-4">
      <p class="text-sm font-medium">DSN
        {#if rotated}<span class="ml-2 inline-flex items-center gap-1 rounded-full bg-green-100 px-2 py-0.5 text-xs font-semibold text-green-700 dark:bg-green-900 dark:text-green-300"><Check class="h-3 w-3" /> Rotated</span>{/if}
      </p>
      <div class="flex gap-2">
        <code class="flex-1 overflow-x-auto rounded-md bg-muted p-2 font-mono text-xs transition-colors {rotated ? 'ring-2 ring-green-500' : ''}">{data.dsn}</code>
        <Button size="sm" variant="outline" on:click={() => data && copy(data.dsn)}><Copy class="h-4 w-4" /></Button>
        <Button size="sm" variant="outline" on:click={rotate}><RefreshCw class="h-4 w-4" /> Rotate</Button>
      </div>
      <p class="text-xs text-muted-foreground">{data.stats.events7d} events / 7d · {data.stats.unresolved} unresolved</p>
    </Card>

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
                <Button size="sm" variant="outline" on:click={() => deleteMap(m.id)}>Delete</Button>
              </div>
            {/each}
          </div>
        {:else}
          <p class="text-xs text-muted-foreground">No source maps uploaded.</p>
        {/if}
      </div>
    </Card>
  </div>
{/if}
