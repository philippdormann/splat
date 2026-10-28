<script lang="ts">
  import { onMount } from "svelte";
  import { AreaChart, BarChart } from "layerchart";
  import { scaleBand } from "d3-scale";
  import { cn } from "$lib/utils";
  import {
    Activity,
    ArrowRight,
    CalendarDays,
    ChevronRight,
    FolderPlus,
    Inbox,
    Minus,
    Sparkles,
    TrendingDown,
    TrendingUp,
    TriangleAlert,
  } from "lucide-svelte";
  import { api, type DashboardData } from "$lib/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Tabs from "$lib/components/ui/tabs/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import * as Empty from "$lib/components/ui/empty/index.js";
  import * as Table from "$lib/components/ui/table/index.js";
  import { Root as Separator } from "$lib/components/ui/separator/index.js";
  import { Root as Skeleton } from "$lib/components/ui/skeleton/index.js";
  import * as Chart from "$lib/components/ui/chart/index.js";
  import Badge from "$lib/components/badge.svelte";
  import Button from "$lib/components/button.svelte";
  import LevelBadge from "$lib/components/LevelBadge.svelte";
  import MeterBar from "$lib/components/MeterBar.svelte";
  import RelativeTime from "$lib/components/RelativeTime.svelte";

  const ranges = [
    { value: "1h", label: "1H" },
    { value: "24h", label: "24H" },
    { value: "7d", label: "7D" },
    { value: "30d", label: "30D" },
  ];

  export let navigate: (path: string) => void;

  let range = "24h";
  let data: DashboardData | null = null;
  let error = "";

  const eventsConfig = { events: { label: "Events", color: "#7c3aed" } } satisfies Chart.ChartConfig;
  const projectConfig = { value: { label: "Events", color: "var(--chart-1)" } } satisfies Chart.ChartConfig;
  const excConfig = { value: { label: "Events", color: "var(--chart-2)" } } satisfies Chart.ChartConfig;

  $: rangeLabel =
    range === "1h"
      ? "Last hour"
      : range === "7d"
        ? "Last 7 days"
        : range === "30d"
          ? "Last 30 days"
          : "Last 24 hours";

  $: delta = data
    ? {
        up: data.stats.eventsDeltaPct > 0,
        down: data.stats.eventsDeltaPct < 0,
      }
    : { up: false, down: false };

  $: statusMax = data
    ? Math.max(1, ...data.charts.byStatus.map((p) => p.value))
    : 1;

  $: envMax = data
    ? Math.max(1, ...data.charts.byEnvironment.slice(0, 5).map((p) => p.value))
    : 1;

  function statusFill(label: string): string {
    switch (label.toLowerCase()) {
      case "resolved":
        return "var(--chart-2)";
      case "ignored":
        return "var(--muted-foreground)";
      case "muted":
        return "var(--chart-4)";
      default:
        return "var(--destructive)";
    }
  }

  async function load() {
    error = "";
    try {
      data = await api.dashboard(range);
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load dashboard.";
    }
  }

  onMount(load);
  $: if (range) load();
</script>

<div class="space-y-6">
  <div class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Overview</h1>
      <p class="text-muted-foreground mt-1 text-sm">{rangeLabel} across all projects</p>
    </div>
    <Tabs.Root bind:value={range}>
      <Tabs.List aria-label="Time range">
        {#each ranges as r}
          <Tabs.Trigger value={r.value}>{r.label}</Tabs.Trigger>
        {/each}
      </Tabs.List>
    </Tabs.Root>
  </div>

  {#if error}
    <Alert.Root variant="destructive">
      <TriangleAlert />
      <Alert.Title>Couldn't load the dashboard</Alert.Title>
      <Alert.Description>{error}</Alert.Description>
    </Alert.Root>
  {:else if !data}
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4" aria-label="Loading dashboard">
      {#each Array(4) as _}
        <Card.Root>
          <Card.Header><Skeleton class="h-4 w-24" /></Card.Header>
          <Card.Content><Skeleton class="h-9 w-20" /></Card.Content>
          <Card.Footer><Skeleton class="h-4 w-32" /></Card.Footer>
        </Card.Root>
      {/each}
    </div>
    <Card.Root>
      <Card.Header><Skeleton class="h-5 w-40" /></Card.Header>
      <Card.Content><Skeleton class="h-44 w-full" /></Card.Content>
    </Card.Root>
  {:else if !data.hasProjects}
    <Empty.Root>
      <Empty.Header>
        <span class="bg-muted flex size-12 items-center justify-center rounded-xl">
          <FolderPlus class="text-muted-foreground size-6" />
        </span>
        <Empty.Title>No projects yet</Empty.Title>
        <Empty.Description>Create a project to get a DSN, then send your first event.</Empty.Description>
      </Empty.Header>
      <Empty.Content>
        <Button on:click={() => navigate("/projects/new")}>Create project</Button>
      </Empty.Content>
    </Empty.Root>
  {:else}
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <Card.Root class="gap-0">
        <Card.Header class="flex-row items-center justify-between pb-2">
          <Card.Description>Total events</Card.Description>
          <span class="bg-muted flex size-8 items-center justify-center rounded-lg">
            <Activity class="text-muted-foreground size-4" />
          </span>
        </Card.Header>
        <Card.Content>
          <p class="text-3xl font-semibold tracking-tight tabular-nums">{data.stats.totalEvents.toLocaleString()}</p>
        </Card.Content>
        <Card.Footer class="pt-3">
          <Badge variant="outline" class={cn("gap-1", delta.up
            ? "border-destructive/30 bg-destructive/10 text-destructive"
            : delta.down
              ? "border-green-600/30 bg-green-600/10 text-green-700 dark:text-green-400"
              : "")}>
            {#if delta.up}<TrendingUp class="size-3" />
            {:else if delta.down}<TrendingDown class="size-3" />
            {:else}<Minus class="size-3" />{/if}
            {data.stats.eventsDeltaLabel}
          </Badge>
          <span class="text-muted-foreground ml-2 text-xs">vs prior {range}</span>
        </Card.Footer>
      </Card.Root>

      <Card.Root class="gap-0">
        <Card.Header class="flex-row items-center justify-between pb-2">
          <Card.Description>Unresolved issues</Card.Description>
          <span class="bg-muted flex size-8 items-center justify-center rounded-lg">
            <TriangleAlert class="text-muted-foreground size-4" />
          </span>
        </Card.Header>
        <Card.Content>
          <p class="text-3xl font-semibold tracking-tight tabular-nums">{data.stats.unresolvedIssues.toLocaleString()}</p>
        </Card.Content>
        <Card.Footer class="pt-3">
          <span class="text-muted-foreground text-xs">{data.stats.newIssues.toLocaleString()} new in this period</span>
        </Card.Footer>
      </Card.Root>

      <Card.Root class="gap-0">
        <Card.Header class="flex-row items-center justify-between pb-2">
          <Card.Description>New issues</Card.Description>
          <span class="bg-muted flex size-8 items-center justify-center rounded-lg">
            <Sparkles class="text-muted-foreground size-4" />
          </span>
        </Card.Header>
        <Card.Content>
          <p class="text-3xl font-semibold tracking-tight tabular-nums">{data.stats.newIssues.toLocaleString()}</p>
        </Card.Content>
        <Card.Footer class="pt-3">
          <span class="text-muted-foreground text-xs">across {data.stats.affectedProjects.toLocaleString()} projects</span>
        </Card.Footer>
      </Card.Root>

      <Card.Root class="gap-0">
        <Card.Header class="flex-row items-center justify-between pb-2">
          <Card.Description>Events today</Card.Description>
          <span class="bg-muted flex size-8 items-center justify-center rounded-lg">
            <CalendarDays class="text-muted-foreground size-4" />
          </span>
        </Card.Header>
        <Card.Content>
          <p class="text-3xl font-semibold tracking-tight tabular-nums">{data.stats.eventsToday.toLocaleString()}</p>
        </Card.Content>
        <Card.Footer class="pt-3">
          <span class="text-muted-foreground text-xs">{data.stats.uniqueIssues.toLocaleString()} unique issues</span>
        </Card.Footer>
      </Card.Root>
    </div>

    {#if data.hasData}
      <div class="grid gap-4 xl:grid-cols-3">
        <Card.Root class="xl:col-span-2">
          <Card.Header>
            <Card.Title>Events over time</Card.Title>
            <Card.Description>Volume for {rangeLabel.toLowerCase()}</Card.Description>
          </Card.Header>
          <Card.Content>
            <Chart.Container config={eventsConfig} class="min-h-[200px] aspect-[2/1]">
              <AreaChart
                data={data.charts.eventsOverTime}
                x="label"
                y="value"
                series={[{ key: "value", label: "Events", color: "#7c3aed" }]}
                props={{ xAxis: { format: (d: string) => d.slice(5) } }}
              >
                {#snippet tooltip()}
                  <Chart.Tooltip labelKey="label" />
                {/snippet}
              </AreaChart>
            </Chart.Container>
          </Card.Content>
        </Card.Root>

        <Card.Root>
          <Card.Header>
            <Card.Title>Breakdown</Card.Title>
            <Card.Description>Issue mix and top environments</Card.Description>
          </Card.Header>
          <Card.Content class="space-y-5">
            <div class="grid grid-cols-2 gap-3">
              <div class="bg-muted/50 rounded-lg p-3">
                <p class="text-2xl font-semibold tracking-tight tabular-nums">{data.charts.newVsRecurring.new.toLocaleString()}</p>
                <p class="text-muted-foreground mt-0.5 text-xs">New issues</p>
              </div>
              <div class="bg-muted/50 rounded-lg p-3">
                <p class="text-2xl font-semibold tracking-tight tabular-nums">{data.charts.newVsRecurring.recurring.toLocaleString()}</p>
                <p class="text-muted-foreground mt-0.5 text-xs">Recurring events</p>
              </div>
            </div>
            <Separator />
            <div class="space-y-3">
              {#each data.charts.byEnvironment.slice(0, 5) as env}
                <div>
                  <div class="mb-1 flex items-baseline justify-between gap-2 text-sm">
                    <span class="truncate">{env.label || "unknown"}</span>
                    <span class="text-muted-foreground shrink-0 tabular-nums">{env.value.toLocaleString()}</span>
                  </div>
                  <MeterBar value={env.value} max={envMax} fill="var(--chart-3)" />
                </div>
              {/each}
            </div>
          </Card.Content>
        </Card.Root>
      </div>

      <div class="grid gap-4 xl:grid-cols-3">
        <Card.Root>
          <Card.Header>
            <Card.Title>By project</Card.Title>
            <Card.Description>Event volume per project</Card.Description>
          </Card.Header>
          <Card.Content>
            <Chart.Container config={projectConfig} class="min-h-[220px]">
              <BarChart
                data={data.charts.byProject}
                xScale={scaleBand().padding(0.25)}
                x="label"
                y="value"
                axis="x"
                series={[{ key: "value", label: "Events", color: "var(--chart-1)" }]}
              >
                {#snippet tooltip()}
                  <Chart.Tooltip labelKey="label" />
                {/snippet}
              </BarChart>
            </Chart.Container>
          </Card.Content>
        </Card.Root>

        <Card.Root>
          <Card.Header>
            <Card.Title>Top exceptions</Card.Title>
            <Card.Description>Most frequent error types</Card.Description>
          </Card.Header>
          <Card.Content>
            <Chart.Container config={excConfig} class="min-h-[220px]">
              <BarChart
                data={data.charts.topExceptions}
                xScale={scaleBand().padding(0.25)}
                x="label"
                y="value"
                axis="x"
                series={[{ key: "value", label: "Events", color: "var(--chart-2)" }]}
              >
                {#snippet tooltip()}
                  <Chart.Tooltip labelKey="label" />
                {/snippet}
              </BarChart>
            </Chart.Container>
          </Card.Content>
        </Card.Root>

        <Card.Root>
          <Card.Header>
            <Card.Title>By status</Card.Title>
            <Card.Description>Active issues per triage state</Card.Description>
          </Card.Header>
          <Card.Content>
            <div class="space-y-4 pt-1">
              {#each data.charts.byStatus as s}
                <div>
                  <div class="mb-1 flex items-baseline justify-between gap-2 text-sm">
                    <span class="capitalize">{s.label}</span>
                    <span class="text-muted-foreground shrink-0 tabular-nums">{s.value.toLocaleString()}</span>
                  </div>
                  <MeterBar value={s.value} max={statusMax} fill={statusFill(s.label)} />
                </div>
              {:else}
                <p class="text-muted-foreground text-sm">No active issues in this range.</p>
              {/each}
            </div>
          </Card.Content>
        </Card.Root>
      </div>

      <Card.Root class="gap-0 overflow-hidden">
        <Card.Header class="flex-row items-center justify-between">
          <div>
            <Card.Title>Recent issues</Card.Title>
            <Card.Description>Latest triage queue</Card.Description>
          </div>
          <Button variant="ghost" size="sm" on:click={() => navigate("/issues")}>
            View all <ArrowRight class="size-4" />
          </Button>
        </Card.Header>
        <Card.Content class="px-2 sm:px-4">
          <Table.Root>
            <Table.Header>
              <Table.Row class="hover:bg-transparent">
                <Table.Head>Issue</Table.Head>
                <Table.Head class="hidden md:table-cell">Project</Table.Head>
                <Table.Head class="text-right">Events</Table.Head>
                <Table.Head class="text-right">Last seen</Table.Head>
                <Table.Head class="w-8"></Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {#each data.recentIssues as i}
                <Table.Row class="cursor-pointer" onclick={() => navigate(`/issues/${i.id}`)}>
                  <Table.Cell>
                    <span class="flex min-w-0 items-center gap-2">
                      <LevelBadge level={i.level} />
                      <span class="truncate font-medium">{i.title}</span>
                    </span>
                  </Table.Cell>
                  <Table.Cell class="hidden md:table-cell">
                    <Badge>{i.projectName}</Badge>
                  </Table.Cell>
                  <Table.Cell class="text-right tabular-nums">{i.eventCount.toLocaleString()}</Table.Cell>
                  <Table.Cell class="text-muted-foreground text-right whitespace-nowrap">
                    <RelativeTime iso={i.lastSeen} />
                  </Table.Cell>
                  <Table.Cell class="text-right">
                    <ChevronRight class="text-muted-foreground ml-auto size-4" />
                  </Table.Cell>
                </Table.Row>
              {/each}
            </Table.Body>
          </Table.Root>
        </Card.Content>
      </Card.Root>
    {:else}
      <Empty.Root>
        <Empty.Header>
          <span class="bg-muted flex size-12 items-center justify-center rounded-xl">
            <Inbox class="text-muted-foreground size-6" />
          </span>
          <Empty.Title>Quiet here</Empty.Title>
          <Empty.Description>No events in this range yet. They'll show up as soon as your apps report in.</Empty.Description>
        </Empty.Header>
      </Empty.Root>
    {/if}
  {/if}
</div>
