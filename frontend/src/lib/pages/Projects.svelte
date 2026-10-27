<script lang="ts">
  import { onMount } from "svelte";
  import { api, type ProjectListItem } from "$lib/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import * as Empty from "$lib/components/ui/empty/index.js";
  import { Root as Skeleton } from "$lib/components/ui/skeleton/index.js";
  import Badge from "$lib/components/badge.svelte";
  import Button from "$lib/components/button.svelte";
  import { Plus, FolderOpen, TriangleAlert, ArrowUpRight } from "lucide-svelte";

  export let navigate: (path: string) => void;

  let projects: ProjectListItem[] | null = null;
  let error = "";

  onMount(async () => {
    try {
      projects = await api.projects();
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load projects.";
    }
  });
</script>

<div class="space-y-6">
  <div class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Projects</h1>
      <p class="text-muted-foreground mt-1 text-sm">
        {#if projects === null}Loading…{:else if projects.length === 0}Nothing here yet{:else}{projects.length} project{projects.length === 1 ? "" : "s"}{/if}
      </p>
    </div>
    <Button size="sm" on:click={() => navigate("/projects/new")}><Plus class="size-4" /> New project</Button>
  </div>

  {#if error}
    <Alert.Root variant="destructive">
      <TriangleAlert />
      <Alert.Title>Couldn't load projects</Alert.Title>
      <Alert.Description>{error}</Alert.Description>
    </Alert.Root>
  {:else if projects === null}
    <div class="grid gap-4 md:grid-cols-2" aria-label="Loading projects">
      {#each Array(4) as _}
        <Card.Root>
          <Card.Header><Skeleton class="h-5 w-32" /></Card.Header>
          <Card.Content><Skeleton class="h-4 w-48" /></Card.Content>
          <Card.Footer><Skeleton class="h-4 w-40" /></Card.Footer>
        </Card.Root>
      {/each}
    </div>
  {:else if projects.length === 0}
    <Empty.Root>
      <Empty.Header>
        <span class="bg-muted flex size-12 items-center justify-center rounded-xl">
          <FolderOpen class="text-muted-foreground size-6" />
        </span>
        <Empty.Title>No projects yet</Empty.Title>
        <Empty.Description>Create one to get a DSN and setup snippet.</Empty.Description>
      </Empty.Header>
      <Empty.Content>
        <Button on:click={() => navigate("/projects/new")}><Plus class="size-4" /> New project</Button>
      </Empty.Content>
    </Empty.Root>
  {:else}
    <div class="grid gap-4 md:grid-cols-2">
      {#each projects as p}
        <button on:click={() => navigate(`/projects/${p.id}`)} class="group text-left">
          <Card.Root class="gap-0 transition-colors group-hover:border-primary">
            <Card.Header class="flex-row items-center justify-between pb-2">
              <Card.Title class="truncate">{p.name}</Card.Title>
              <span class="flex shrink-0 items-center gap-2">
                {#if !p.enabled}<Badge variant="outline">disabled</Badge>{/if}
                <ArrowUpRight class="text-muted-foreground size-4 transition-transform group-hover:translate-x-0.5 group-hover:-translate-y-0.5" />
              </span>
            </Card.Header>
            <Card.Content>
              <p class="truncate font-mono text-xs text-muted-foreground">{p.slug} · {p.platformLabel}</p>
            </Card.Content>
            <Card.Footer class="gap-3 pt-3">
              <span class="text-sm font-semibold tabular-nums">{p.events7d.toLocaleString()}</span>
              <span class="text-muted-foreground text-xs">events / 7d</span>
              <span class="text-muted-foreground text-xs">·</span>
              <span class="text-sm font-semibold tabular-nums">{p.unresolved.toLocaleString()}</span>
              <span class="text-muted-foreground text-xs">unresolved</span>
            </Card.Footer>
          </Card.Root>
        </button>
      {/each}
    </div>
  {/if}
</div>
