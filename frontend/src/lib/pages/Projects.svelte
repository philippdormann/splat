<script lang="ts">
  import { onMount } from "svelte";
  import { api, type ProjectListItem } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Badge from "$lib/components/badge.svelte";
  import Button from "$lib/components/button.svelte";
  import { Plus } from "lucide-svelte";

  export let navigate: (path: string) => void;

  let projects: ProjectListItem[] = [];
  let error = "";

  onMount(async () => {
    try {
      projects = await api.projects();
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load projects.";
    }
  });
</script>

<div class="space-y-4">
  <div class="flex items-center justify-between">
    <h2 class="text-2xl font-bold">Projects</h2>
    <Button size="sm" on:click={() => navigate("/projects/new")}><Plus class="h-4 w-4" /> New project</Button>
  </div>
  {#if error}<p class="text-sm text-destructive">{error}</p>{/if}
  <div class="grid gap-3 md:grid-cols-2">
    {#each projects as p}
      <button on:click={() => navigate(`/projects/${p.id}`)} class="text-left">
        <Card cls="p-4 hover:border-primary">
          <div class="flex items-center justify-between gap-2">
            <p class="font-semibold">{p.name}</p>
            {#if !p.enabled}<Badge variant="outline">disabled</Badge>{/if}
          </div>
          <p class="mt-1 font-mono text-xs text-muted-foreground">{p.slug} · {p.platformLabel}</p>
          <p class="mt-2 text-xs text-muted-foreground">{p.events7d} events / 7d · {p.unresolved} unresolved</p>
        </Card>
      </button>
    {:else}
      <p class="text-sm text-muted-foreground">No projects yet.</p>
    {/each}
  </div>
</div>
