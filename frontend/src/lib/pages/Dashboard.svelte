<script lang="ts">
  import { onMount } from "svelte";
  import { Chart, registerables } from "chart.js";
  import { api, type DashboardData } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Badge from "$lib/components/badge.svelte";
  import Button from "$lib/components/button.svelte";
  import RelativeTime from "$lib/components/RelativeTime.svelte";

  Chart.register(...registerables);

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
  let charts: Chart[] = [];

  const dark = () => document.documentElement.classList.contains("dark");

  function makeChart(id: string, cfg: unknown) {
    const el = document.getElementById(id) as HTMLCanvasElement | null;
    if (!el || !cfg) return;
    const prev = charts.find((c) => c.canvas.id === id);
    prev?.destroy();
    charts = charts.filter((c) => c.canvas.id !== id);
    charts.push(new Chart(el, cfg as never));
  }

  async function load() {
    error = "";
    try {
      data = await api.dashboard(range);
      queueMicrotask(() => {
        if (!data) return;
        const grid = dark() ? "#3f3f46" : "#e4e4e7";
        const tick = dark() ? "#a1a1aa" : "#71717a";
        const base = { plugins: { legend: { display: false } }, scales: { y: { beginAtZero: true, grid: { color: grid }, ticks: { color: tick } }, x: { grid: { display: false }, ticks: { color: tick, maxTicksLimit: 8 } } } };
        makeChart("ch-events", { type: "line", data: { labels: data.charts.eventsOverTime.map((p) => p.label), datasets: [{ data: data.charts.eventsOverTime.map((p) => p.value), borderColor: "#7c3aed", tension: 0.25, pointRadius: 0 }] }, options: base });
        makeChart("ch-project", { type: "bar", data: { labels: data.charts.byProject.map((p) => p.label), datasets: [{ data: data.charts.byProject.map((p) => p.value), backgroundColor: "#7c3aed" }] }, options: base });
        makeChart("ch-exc", { type: "bar", data: { labels: data.charts.topExceptions.map((p) => p.label), datasets: [{ data: data.charts.topExceptions.map((p) => p.value), backgroundColor: "#dc2626" }] }, options: base });
        makeChart("ch-env", { type: "doughnut", data: { labels: data.charts.byEnvironment.map((p) => p.label), datasets: [{ data: data.charts.byEnvironment.map((p) => p.value), backgroundColor: ["#7c3aed", "#dc2626", "#16a34a", "#d97706", "#0891b2"] }] }, options: { plugins: { legend: { position: "bottom", labels: { color: tick } } } } });
      });
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load dashboard.";
    }
  }

  onMount(load);
  $: if (range) load();
  import { onDestroy } from "svelte";
  onDestroy(() => charts.forEach((c) => c.destroy()));
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
        <Card cls="p-4"><p class="mb-2 text-sm font-medium">Events over time</p><canvas id="ch-events" height="120"></canvas></Card>
        <Card cls="p-4"><p class="mb-2 text-sm font-medium">By project</p><canvas id="ch-project" height="120"></canvas></Card>
        <Card cls="p-4"><p class="mb-2 text-sm font-medium">Top exceptions</p><canvas id="ch-exc" height="120"></canvas></Card>
        <Card cls="p-4"><p class="mb-2 text-sm font-medium">By environment</p><canvas id="ch-env" height="120"></canvas></Card>
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
