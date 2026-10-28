<script lang="ts">
  import { onMount } from "svelte";
  import { Rocket, ArrowLeft, TriangleAlert, ChevronRight } from "lucide-svelte";
  import { api, type ReleaseDetail } from "$lib/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import * as Table from "$lib/components/ui/table/index.js";
  import { Root as Skeleton } from "$lib/components/ui/skeleton/index.js";
  import LevelBadge from "$lib/components/LevelBadge.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import DateTime from "$lib/components/DateTime.svelte";

  export let id: string;
  export let release: string;
  export let navigate: (path: string) => void;

  let data: ReleaseDetail | null = null;
  let error = "";

  function tier(pct: number): string {
    if (pct >= 99) return "text-green-600 dark:text-green-400";
    if (pct >= 95) return "text-amber-600 dark:text-amber-400";
    return "text-destructive";
  }

  onMount(async () => {
    try {
      data = await api.projectRelease(id, release);
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load release.";
    }
  });
</script>

<div class="mx-auto w-full max-w-6xl space-y-6">
  <button class="inline-flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground" on:click={() => navigate(`/projects/${id}/releases`)}>
    <ArrowLeft class="h-4 w-4" /> Back to releases
  </button>
  <div class="flex items-center gap-3">
    <span class="bg-muted flex size-9 items-center justify-center rounded-lg">
      <Rocket class="text-muted-foreground size-5" />
    </span>
    <div class="min-w-0">
      <h1 class="truncate font-mono text-2xl font-semibold tracking-tight">{release}</h1>
      <p class="text-muted-foreground mt-1 text-sm">
        {#if data}
          Active since <DateTime iso={data.windowStart} />{#if data.windowEnd} · superseded <DateTime iso={data.windowEnd} />{:else} · current release{/if}
        {:else}Release detail{/if}
      </p>
    </div>
  </div>

  {#if error}
    <Alert.Root variant="destructive">
      <TriangleAlert />
      <Alert.Title>Couldn't load the release</Alert.Title>
      <Alert.Description>{error}</Alert.Description>
    </Alert.Root>
  {:else if !data}
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4" aria-label="Loading release">
      {#each Array(4) as _}
        <Card.Root>
          <Card.Header><Skeleton class="h-4 w-24" /></Card.Header>
          <Card.Content><Skeleton class="h-9 w-20" /></Card.Content>
        </Card.Root>
      {/each}
    </div>
  {:else}
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <Card.Root class="gap-0">
        <Card.Header class="pb-2"><Card.Description>Crash-free sessions</Card.Description></Card.Header>
        <Card.Content>
          <p class="text-3xl font-semibold tracking-tight tabular-nums {tier(data.summary.crashFreeSessions)}">{data.summary.sessions > 0 ? `${data.summary.crashFreeSessions.toFixed(1)}%` : "—"}</p>
        </Card.Content>
        <Card.Footer class="pt-3">
          <span class="text-muted-foreground text-xs tabular-nums">{data.summary.sessions.toLocaleString()} sessions · {data.summary.users.toLocaleString()} users</span>
        </Card.Footer>
      </Card.Root>
      <Card.Root class="gap-0">
        <Card.Header class="pb-2"><Card.Description>Events</Card.Description></Card.Header>
        <Card.Content>
          <p class="text-3xl font-semibold tracking-tight tabular-nums">{data.summary.events.toLocaleString()}</p>
        </Card.Content>
        <Card.Footer class="pt-3">
          <span class="text-muted-foreground text-xs tabular-nums">{data.summary.issues.toLocaleString()} issues · {data.summary.transactions.toLocaleString()} traces</span>
        </Card.Footer>
      </Card.Root>
      <Card.Root class="gap-0">
        <Card.Header class="pb-2"><Card.Description>New issues</Card.Description></Card.Header>
        <Card.Content>
          <p class="text-3xl font-semibold tracking-tight tabular-nums">{data.newIssues.length.toLocaleString()}</p>
        </Card.Content>
        <Card.Footer class="pt-3">
          <span class="text-muted-foreground text-xs">first seen while this release was newest</span>
        </Card.Footer>
      </Card.Root>
      <Card.Root class="gap-0">
        <Card.Header class="pb-2"><Card.Description>Environments</Card.Description></Card.Header>
        <Card.Content>
          <p class="text-3xl font-semibold tracking-tight tabular-nums">{data.environments.length.toLocaleString()}</p>
        </Card.Content>
        <Card.Footer class="pt-3">
          <span class="text-muted-foreground text-xs">with activity in this release</span>
        </Card.Footer>
      </Card.Root>
    </div>

    <Card.Root class="gap-0 overflow-hidden">
      <Card.Header>
        <Card.Title>Environments</Card.Title>
        <Card.Description>Events and sessions per environment in this release</Card.Description>
      </Card.Header>
      <Card.Content class="px-2 sm:px-4">
        {#if data.environments.length === 0}
          <p class="text-muted-foreground py-4 text-center text-sm">No environment data.</p>
        {:else}
          <Table.Root>
            <Table.Header>
              <Table.Row class="hover:bg-transparent">
                <Table.Head>Environment</Table.Head>
                <Table.Head class="text-right">Events</Table.Head>
                <Table.Head class="text-right">Sessions</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {#each data.environments as e}
                <Table.Row>
                  <Table.Cell class="font-medium">{e.environment}</Table.Cell>
                  <Table.Cell class="text-right tabular-nums">{e.events.toLocaleString()}</Table.Cell>
                  <Table.Cell class="text-right tabular-nums">{e.sessions.toLocaleString()}</Table.Cell>
                </Table.Row>
              {/each}
            </Table.Body>
          </Table.Root>
        {/if}
      </Card.Content>
    </Card.Root>

    <Card.Root class="gap-0 overflow-hidden">
      <Card.Header>
        <Card.Title>New in this release ({data.newIssues.length})</Card.Title>
        <Card.Description>Issues first seen while this release was the newest</Card.Description>
      </Card.Header>
      <Card.Content class="px-2 sm:px-4">
        {#if data.newIssues.length === 0}
          <p class="text-muted-foreground py-4 text-center text-sm">Nothing new broke in this release. Enjoy the quiet.</p>
        {:else}
          <Table.Root>
            <Table.Header>
              <Table.Row class="hover:bg-transparent">
                <Table.Head>Issue</Table.Head>
                <Table.Head>Status</Table.Head>
                <Table.Head class="text-right">Events</Table.Head>
                <Table.Head class="text-right">First seen</Table.Head>
                <Table.Head class="w-8"></Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {#each data.newIssues as i}
                <Table.Row class="cursor-pointer" onclick={() => navigate(`/issues/${i.id}`)}>
                  <Table.Cell>
                    <span class="flex min-w-0 items-center gap-2">
                      <LevelBadge level={i.level} />
                      <span class="truncate font-medium">{i.title}</span>
                    </span>
                  </Table.Cell>
                  <Table.Cell>
                    <StatusBadge status={i.status} />
                  </Table.Cell>
                  <Table.Cell class="text-right tabular-nums">{i.eventCount.toLocaleString()}</Table.Cell>
                  <Table.Cell class="text-muted-foreground text-right whitespace-nowrap">
                    <DateTime iso={i.firstSeen} />
                  </Table.Cell>
                  <Table.Cell class="text-right">
                    <ChevronRight class="text-muted-foreground ml-auto size-4" />
                  </Table.Cell>
                </Table.Row>
              {/each}
            </Table.Body>
          </Table.Root>
        {/if}
      </Card.Content>
    </Card.Root>
  {/if}
</div>
