<script lang="ts">
  import { onMount } from "svelte";
  import { Rocket, ArrowLeft } from "lucide-svelte";
  import { api, type ReleaseHealth } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Button from "$lib/components/button.svelte";
  import DateTime from "$lib/components/DateTime.svelte";
  import PlatformIcon from "$lib/components/PlatformIcon.svelte";

  export let id: string;
  export let navigate: (path: string) => void;

  let projectName = "";
  let platform = "";
  let releases: ReleaseHealth[] | null = null;
  let error = "";

  function tier(pct: number): string {
    if (pct >= 99) return "text-green-600 dark:text-green-400";
    if (pct >= 95) return "text-amber-600 dark:text-amber-400";
    return "text-destructive";
  }

  onMount(async () => {
    try {
      const [p, r] = await Promise.all([api.project(id), api.projectReleases(id)]);
      projectName = p.project.name;
      platform = p.project.platform;
      releases = r;
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load releases.";
    }
  });
</script>

<div class="mx-auto w-full max-w-6xl space-y-4">
  <button class="inline-flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground" on:click={() => navigate(`/projects/${id}`)}>
    <ArrowLeft class="h-4 w-4" /> Back to project
  </button>
  <div class="flex items-center gap-3">
    <PlatformIcon {platform} size={28} />
    <div>
      <h2 class="text-xl font-extrabold tracking-tight md:text-2xl">{projectName || "…"}</h2>
      <p class="text-sm text-muted-foreground">Releases</p>
    </div>
  </div>

  {#if error}
    <p class="text-sm text-destructive">{error}</p>
  {:else if releases === null}
    <div class="space-y-2" aria-label="Loading releases">
      <div class="h-12 animate-pulse rounded-lg bg-muted" />
      <div class="h-12 animate-pulse rounded-lg bg-muted" />
      <div class="h-12 animate-pulse rounded-lg bg-muted" />
    </div>
  {:else if releases.length === 0}
    <Card cls="p-6"><p class="text-sm text-muted-foreground">No releases seen yet. Events and sessions record the release they came from.</p></Card>
  {:else}
    <Card cls="overflow-x-auto p-0">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b border-border text-left text-xs text-muted-foreground">
            <th class="p-3 font-medium">Release</th>
            <th class="p-3 text-right font-medium">Events</th>
            <th class="p-3 text-right font-medium">Issues</th>
            <th class="p-3 text-right font-medium">Sessions</th>
            <th class="p-3 text-right font-medium">Crash-free</th>
            <th class="p-3 text-right font-medium">Last seen</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-border">
          {#each releases as r}
            <tr class="hover:bg-muted/50">
              <td class="p-3 font-mono text-xs font-semibold">
                <span class="inline-flex items-center gap-1.5"><Rocket class="h-3.5 w-3.5 text-muted-foreground" />{r.release}</span>
              </td>
              <td class="p-3 text-right tabular-nums">{r.events.toLocaleString()}</td>
              <td class="p-3 text-right tabular-nums">{r.issues.toLocaleString()}</td>
              <td class="p-3 text-right tabular-nums">{r.sessions > 0 ? r.sessions.toLocaleString() : "—"}</td>
              <td class="p-3 text-right tabular-nums">
                {#if r.sessions > 0}
                  <span class={tier(r.crashFreeSessions)}>{r.crashFreeSessions.toFixed(1)}%</span>
                  <span class="text-muted-foreground"> · </span>
                  <span class={tier(r.crashFreeUsers)}>{r.crashFreeUsers.toFixed(1)}% users</span>
                {:else}—{/if}
              </td>
              <td class="p-3 text-right text-xs text-muted-foreground">{#if r.lastSeen}<DateTime iso={r.lastSeen} />{:else}—{/if}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </Card>
    <div class="flex gap-2">
      <Button size="sm" variant="outline" on:click={() => navigate(`/projects/${id}`)}>Project overview</Button>
      <Button size="sm" variant="outline" on:click={() => navigate("/issues")}>All issues</Button>
    </div>
  {/if}
</div>
