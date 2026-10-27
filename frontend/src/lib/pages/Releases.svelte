<script lang="ts">
  import { onMount } from "svelte";
  import { Rocket, ArrowLeft, TriangleAlert, Tag } from "lucide-svelte";
  import { api, type ReleaseHealth } from "$lib/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import * as Empty from "$lib/components/ui/empty/index.js";
  import * as Table from "$lib/components/ui/table/index.js";
  import { Root as Skeleton } from "$lib/components/ui/skeleton/index.js";
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

<div class="mx-auto w-full max-w-6xl space-y-6">
  <button class="inline-flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground" on:click={() => navigate(`/projects/${id}`)}>
    <ArrowLeft class="h-4 w-4" /> Back to project
  </button>
  <div class="flex items-center gap-3">
    <PlatformIcon {platform} size={28} />
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">{projectName || "…"}</h1>
      <p class="text-muted-foreground mt-1 text-sm">Release health across {releases?.length ?? "…"} releases</p>
    </div>
  </div>

  {#if error}
    <Alert.Root variant="destructive">
      <TriangleAlert />
      <Alert.Title>Couldn't load releases</Alert.Title>
      <Alert.Description>{error}</Alert.Description>
    </Alert.Root>
  {:else if releases === null}
    <Card.Root>
      <Card.Content class="space-y-2 pt-6" aria-label="Loading releases">
        <Skeleton class="h-12 w-full" />
        <Skeleton class="h-12 w-full" />
        <Skeleton class="h-12 w-full" />
      </Card.Content>
    </Card.Root>
  {:else if releases.length === 0}
    <Empty.Root>
      <Empty.Header>
        <span class="bg-muted flex size-12 items-center justify-center rounded-xl">
          <Tag class="text-muted-foreground size-6" />
        </span>
        <Empty.Title>No releases yet</Empty.Title>
        <Empty.Description>Events and sessions record the release they came from.</Empty.Description>
      </Empty.Header>
    </Empty.Root>
  {:else}
    <Card.Root class="gap-0 overflow-hidden">
      <Card.Header>
        <Card.Title>Releases</Card.Title>
        <Card.Description>Most recently active first</Card.Description>
      </Card.Header>
      <Card.Content class="px-2 sm:px-4">
        <Table.Root>
          <Table.Header>
            <Table.Row class="hover:bg-transparent">
              <Table.Head>Release</Table.Head>
              <Table.Head class="text-right">Events</Table.Head>
              <Table.Head class="text-right">Issues</Table.Head>
              <Table.Head class="text-right">Sessions</Table.Head>
              <Table.Head class="text-right">Traces</Table.Head>
              <Table.Head class="text-right">Crash-free</Table.Head>
              <Table.Head class="text-right">Last seen</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {#each releases as r}
              <Table.Row>
                <Table.Cell>
                  <button class="inline-flex items-center gap-1.5 font-mono text-xs font-semibold hover:underline" on:click={() => navigate(`/projects/${id}/releases/${encodeURIComponent(r.release)}`)}>
                    <Rocket class="text-muted-foreground size-3.5" />{r.release}
                  </button>
                </Table.Cell>
                <Table.Cell class="text-right tabular-nums">{r.events.toLocaleString()}</Table.Cell>
                <Table.Cell class="text-right tabular-nums">{r.issues.toLocaleString()}</Table.Cell>
                <Table.Cell class="text-right tabular-nums">{r.sessions > 0 ? r.sessions.toLocaleString() : "—"}</Table.Cell>
                <Table.Cell class="text-right tabular-nums">{r.transactions > 0 ? r.transactions.toLocaleString() : "—"}</Table.Cell>
                <Table.Cell class="text-right tabular-nums">
                  {#if r.sessions > 0}
                    <span class={tier(r.crashFreeSessions)}>{r.crashFreeSessions.toFixed(1)}%</span>
                    <span class="text-muted-foreground"> · </span>
                    <span class={tier(r.crashFreeUsers)}>{r.crashFreeUsers.toFixed(1)}% users</span>
                  {:else}—{/if}
                </Table.Cell>
                <Table.Cell class="text-muted-foreground text-right text-xs">{#if r.lastSeen}<DateTime iso={r.lastSeen} />{:else}—{/if}</Table.Cell>
              </Table.Row>
            {/each}
          </Table.Body>
        </Table.Root>
      </Card.Content>
    </Card.Root>
    <div class="flex gap-2">
      <Button size="sm" variant="outline" on:click={() => navigate(`/projects/${id}`)}>Project overview</Button>
      <Button size="sm" variant="outline" on:click={() => navigate("/issues")}>All issues</Button>
    </div>
  {/if}
</div>
