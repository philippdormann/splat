<script lang="ts">
  import { onMount } from "svelte";
  import { api, type ProjectDetail } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import { Copy, RefreshCw, Trash2 } from "lucide-svelte";

  export let id: string;
  export let navigate: (path: string) => void;

  let data: ProjectDetail | null = null;
  let error = "";
  let notice = "";

  async function load() {
    try {
      data = await api.project(id);
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load project.";
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
    notice = "Saved.";
  }

  async function rotate() {
    const r = await api.rotateProjectKey(id);
    if (data) data = { ...data, dsn: r.dsn };
    notice = "DSN rotated.";
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
      <p class="text-sm font-medium">DSN</p>
      <div class="flex gap-2">
        <code class="flex-1 overflow-x-auto rounded-md bg-muted p-2 font-mono text-xs">{data.dsn}</code>
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
          <div class="space-y-1"><label class="text-xs">Retention (days)</label><Input type="number" bind:value={data.project.retentionDays} /></div>
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
  </div>
{/if}
