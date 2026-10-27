<script lang="ts">
  import { onMount } from "svelte";
  import { AreaChart, BarChart } from "layerchart";
  import { scaleBand } from "d3-scale";
  import { api, type DashboardData } from "$lib/api";
  import * as Chart from "$lib/components/ui/chart/index.js";
  import Card from "$lib/components/card.svelte";
  import Badge from "$lib/components/badge.svelte";
  import Button from "$lib/components/button.svelte";
  import RelativeTime from "$lib/components/RelativeTime.svelte";

  const ranges = [
    { value: "1h", label: "Last hour" },
    { value: "24h", label: "Last 24 hours" },
    { value: "7d", label: "Last 7 days" },
    { value: "30d", label: "Last 30 days" },
  ];

  export let navigate: (path: string) => void;

  let range = "24h";
  let data: DashboardData | null = null;
  let error = "";

  const eventsConfig = { events: { label: "Events", color: "#7c3aed" } } satisfies Chart.ChartConfig;
  const projectConfig = { value: { label: "Events", color: "var(--chart-1)" } } satisfies Chart.ChartConfig;
  const excConfig = { value: { label: "Events", color: "var(--chart-2)" } } satisfies Chart.ChartConfig;
  const envConfig = { value: { label: "Events", color: "var(--chart-3)" } } satisfies Chart.ChartConfig;

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
  <div class="flex flex-wrap items-center justify-between gap-3">
    <h2 class="text-2xl font-bold">Dashboard</h2>
    <div class="flex gap-2">
      {#each ranges as r}
        <Button variant={range === r.value ? "default" : "outline"} size="sm" on:click={() => (range = r.value)}>{r.label}</Button>
      {/each}
    </div>
  </div>

  {#if error}
    <p class="text-sm text-destructive">{error}</p>
  {:else if !data}
    <div class="space-y-3" aria-label="Loading dashboard">
      <div class="h-7 w-48 animate-pulse rounded bg-muted"></div>
      <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <div class="h-20 animate-pulse rounded-lg bg-muted"></div>
        <div class="h-20 animate-pulse rounded-lg bg-muted"></div>
        <div class="h-20 animate-pulse rounded-lg bg-muted"></div>
        <div class="h-20 animate-pulse rounded-lg bg-muted"></div>
      </div>
      <div class="h-64 animate-pulse rounded-lg bg-muted"></div>
    </div>
  {:else if !data.hasProjects}
    <Card cls="p-6">
      <p class="font-medium">No projects yet</p>
      <p class="mt-1 text-sm text-muted-foreground">Create a project to get a DSN, then send your first event.</p>
      <div class="mt-4"><Button on:click={() => navigate("/projects/new")}>Create project</Button></div>
    </Card>
  {:else}
    <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <Card cls="p-4"><p class="text-xs text-muted-foreground">Events ({range})</p><p class="text-2xl font-bold">{data.stats.totalEvents}</p><p class="text-xs text-muted-foreground">{data.stats.eventsDeltaLabel}</p></Card>
      <Card cls="p-4"><p class="text-xs text-muted-foreground">Unresolved issues</p><p class="text-2xl font-bold">{data.stats.unresolvedIssues}</p></Card>
      <Card cls="p-4"><p class="text-xs text-muted-foreground">New issues</p><p class="text-2xl font-bold">{data.stats.newIssues}</p></Card>
      <Card cls="p-4"><p class="text-xs text-muted-foreground">Events today</p><p class="text-2xl font-bold">{data.stats.eventsToday}</p></Card>
    </div>

    {#if data.hasData}
      <div class="grid gap-3 lg:grid-cols-2">
        <Card cls="p-4">
          <p class="mb-2 text-sm font-medium">Events over time</p>
          <Chart.Container config={eventsConfig} class="min-h-[220px]">
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
        </Card>
        <Card cls="p-4">
          <p class="mb-2 text-sm font-medium">By project</p>
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
        </Card>
        <Card cls="p-4">
          <p class="mb-2 text-sm font-medium">Top exceptions</p>
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
        </Card>
        <Card cls="p-4">
          <p class="mb-2 text-sm font-medium">By environment</p>
          <Chart.Container config={envConfig} class="min-h-[220px]">
            <BarChart
              data={data.charts.byEnvironment}
              xScale={scaleBand().padding(0.25)}
              x="label"
              y="value"
              axis="x"
              series={[{ key: "value", label: "Events", color: "var(--chart-3)" }]}
            >
              {#snippet tooltip()}
                <Chart.Tooltip labelKey="label" />
              {/snippet}
            </BarChart>
          </Chart.Container>
        </Card>
      </div>

      <Card cls="p-4">
        <p class="mb-3 text-sm font-medium">Recent issues</p>
        <div class="divide-y divide-border">
          {#each data.recentIssues as i}
            <button class="flex w-full items-center justify-between gap-3 py-2 text-left hover:underline" on:click={() => navigate(`/issues/${i.id}`)}>
              <span class="truncate text-sm font-medium">{i.title}</span>
              <span class="flex shrink-0 items-center gap-2 text-xs text-muted-foreground">
                <Badge>{i.projectName}</Badge>
                <span>{i.eventCount} events · <RelativeTime iso={i.lastSeen} /></span>
              </span>
            </button>
          {/each}
        </div>
      </Card>
    {:else}
      <Card cls="p-6"><p class="text-sm text-muted-foreground">No events in this range yet.</p></Card>
    {/if}
  {/if}
</div>
