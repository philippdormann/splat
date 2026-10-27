<script lang="ts">
  import { onMount } from "svelte";
  import { api, ApiError, type Platform } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";

  export let navigate: (path: string) => void;

  let name = "";
  let slug = "";
  let defaultEnvironment = "production";
  let platform = "";
  let retentionDays = 90;
  let platforms: Platform[] = [];
  let error = "";
  let busy = false;

  onMount(async () => {
    platforms = await api.platforms();
    if (platforms.length && !platform) platform = platforms[0].id;
  });

  function slugify() {
    slug = name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
  }

  async function submit(e: Event) {
    e.preventDefault();
    busy = true;
    error = "";
    try {
      const created = await api.createProject({ name, slug, defaultEnvironment, platform, retentionDays });
      navigate(`/projects/${created.project.id}`);
    } catch (err) {
      error = err instanceof ApiError ? err.message : "Could not create project.";
    } finally {
      busy = false;
    }
  }
</script>

<div class="mx-auto max-w-lg space-y-4">
  <h2 class="text-2xl font-bold">New project</h2>
  <Card cls="p-6">
    <form on:submit={submit} class="space-y-4">
      <div class="space-y-2"><label class="text-sm font-medium">Name</label><Input bind:value={name} on:input={slugify} required placeholder="My app" /></div>
      <div class="space-y-2"><label class="text-sm font-medium">Slug</label><Input bind:value={slug} required placeholder="my-app" /></div>
      <div class="grid grid-cols-2 gap-3">
        <div class="space-y-2"><label class="text-sm font-medium">Environment</label><Input bind:value={defaultEnvironment} placeholder="production" /></div>
        <div class="space-y-2"><label class="text-sm font-medium">Retention (days)</label><Input type="number" bind:value={retentionDays} /></div>
      </div>
      <div class="space-y-2">
        <label class="text-sm font-medium">Platform</label>
        <select class="h-9 w-full rounded-md border border-input bg-background px-2 text-sm" bind:value={platform}>
          {#each platforms as p}<option value={p.id}>{p.label}</option>{/each}
        </select>
      </div>
      {#if error}<p class="text-sm text-destructive">{error}</p>{/if}
      <Button type="submit" disabled={busy} class="w-full">{busy ? "Creating…" : "Create project"}</Button>
    </form>
  </Card>
</div>
