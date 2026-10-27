<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { Chart, registerables } from "chart.js";
  import { Activity, HeartPulse, Timer, Trophy, Zap, CircleDot, Gauge } from "lucide-svelte";
  import { api, type ProjectOverview, type SessionSummary } from "$lib/api";
  import Button from "$lib/components/button.svelte";
  import Tile from "$lib/components/Tile.svelte";
  import TileSkeleton from "$lib/components/TileSkeleton.svelte";
  import StatTile from "$lib/components/StatTile.svelte";
  import LevelBadge from "$lib/components/LevelBadge.svelte";

  Chart.register(...registerables);

  export let projectId: string;
  export let navigate: (path: string) => void;

  const periods = [
    { value: "24h", label: "24h" },
    { value: "7d", label: "7d" },
    { value: "30d", label: "30d" },
  ];

  const levelStyle: Record<string, { color: string; order: number }> = {
    error: { color: "#ef4444", order: 0 },
    fatal: { color: "#b91c1c", order: 1 },
    warning: { color: "#f59e0b", order: 2 },
    info: { color: "#3b82f6", order: 3 },
    debug: { color: "#9ca3af", order: 4 },
  };

  let period = "24h";
  let data: ProjectOverview | null = null;
  let sessions: SessionSummary | null = null;
  let error = "";
  let charts: Chart[] = [];

  const dark = () => document.documentElement.classList.contains("dark");

  function tierColor(pct: number | null): string {
    if (pct === null || pct === undefined) return "text-muted-foreground";
    if (pct >= 99) return "text-green-600 dark:text-green-400";
    if (pct >= 95) return "text-amber-600 dark:text-amber-400";
    return "text-destructive";
  }

  function fmtPct(pct: number | null): string {
    if (pct === null || pct === undefined) return "—";
    return `${pct.toFixed(1)}%`;
  }

  function levelDatasets() {
    if (!data) return [];
    const levels = new Set<string>();
    for (const b of data.volumeBySeverity) for (const l of Object.keys(b.counts)) levels.add(l);
    const ordered = [...levels].sort((a, b) => {
      const ma = levelStyle[a.toLowerCase()] ?? { order: 5 };
      const mb = levelStyle[b.toLowerCase()] ?? { order: 5 };
      return ma.order - mb.order || a.localeCompare(b);
    });
    return ordered.map((level) => {
      const color = (levelStyle[level.toLowerCase()] ?? { color: "#8b5cf6" }).color;
      return {
        label: level,
        data: data!.volumeBySeverity.map((bucket) => bucket.counts[level] ?? 0),
        borderColor: color,
        backgroundColor: color + "55",
        fill: true,
        tension: 0.25,
        pointRadius: 0,
        stack: "volume",
      };
    });
  }

  function drawChart(id: string, cfg: unknown) {
    const el = document.getElementById(id) as HTMLCanvasElement | null;
    if (!el) return;
    const prev = charts.find((c) => c.canvas.id === id);
    prev?.destroy();
    charts = charts.filter((c) => c.canvas.id !== id);
    charts.push(new Chart(el, cfg as never));
  }

  function draw() {
    if (!data) return;
    const grid = dark() ? "#3f3f46" : "#e4e4e7";
    const tick = dark() ? "#a1a1aa" : "#71717a";
    if (data.volumeBySeverity.length) {
      drawChart("ov-volume", {
        type: "line",
        data: { labels: data.volumeBySeverity.map((b) => b.bucket), datasets: levelDatasets() },
        options: {
          plugins: { legend: { position: "bottom", labels: { color: tick, boxWidth: 12 } } },
          scales: {
            y: { stacked: true, beginAtZero: true, grid: { color: grid }, ticks: { color: tick, precision: 0 } },
            x: { stacked: true, grid: { display: false }, ticks: { color: tick, maxTicksLimit: 8 } },
          },
        },
      });
    }
    if (sessions && sessions.timeseries.length) {
      drawChart("ov-sessions", {
        type: "line",
        data: {
          labels: sessions.timeseries.map((p) => p.bucket),
          datasets: [
            { label: "Healthy", data: sessions.timeseries.map((p) => p.healthy), borderColor: "#16a34a", backgroundColor: "#16a34a55", fill: true, tension: 0.25, pointRadius: 0, stack: "sess" },
            { label: "Crashed", data: sessions.timeseries.map((p) => p.crashed), borderColor: "#dc2626", backgroundColor: "#dc262655", fill: true, tension: 0.25, pointRadius: 0, stack: "sess" },
          ],
        },
        options: {
          plugins: { legend: { position: "bottom", labels: { color: tick, boxWidth: 12 } } },
          scales: {
            y: { stacked: true, beginAtZero: true, grid: { color: grid }, ticks: { color: tick, precision: 0 } },
            x: { stacked: true, grid: { display: false }, ticks: { color: tick, maxTicksLimit: 8 } },
          },
        },
      });
    }
  }

  async function load() {
    error = "";
    try {
      const [ov, sess] = await Promise.all([
        api.projectOverview(projectId, period),
        api.projectSessionSummary(projectId, period),
      ]);
      data = ov;
      sessions = sess;
      queueMicrotask(draw);
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load overview.";
    }
  }

  onMount(load);
  $: if (period) load();
  onDestroy(() => charts.forEach((c) => c.destroy()));
</script>

<div class="space-y-4">
  <div class="flex flex-wrap items-center justify-between gap-3">
    <p class="text-sm font-semibold">Overview</p>
    <div class="flex gap-2">
      {#each periods as p}
        <Button variant={period === p.value ? "default" : "outline"} size="sm" on:click={() => (period = p.value)}>{p.label}</Button>
      {/each}
    </div>
  </div>

  {#if error}
    <p class="text-sm text-destructive">{error}</p>
  {:else if !data}
    <div class="grid gap-4 xl:grid-cols-4">
      <div class="min-w-0 xl:col-span-2"><TileSkeleton height={300} /></div>
      <div class="grid min-w-0 gap-4 sm:grid-cols-2 xl:col-span-2">
        <TileSkeleton height={56} />
        <TileSkeleton height={56} />
        <div class="min-w-0 sm:col-span-2"><TileSkeleton height={132} /></div>
      </div>
    </div>
  {:else}
    <div class="grid gap-4 xl:grid-cols-4">
      <div class="min-w-0 xl:col-span-2">
        <Tile icon={Activity}>
          <span slot="title">Error volume by severity</span>
          {#if data.volumeBySeverity.length}
            <canvas id="ov-volume" height="300"></canvas>
          {:else}
            <p class="py-10 text-center text-sm text-muted-foreground">No events in this window.</p>
          {/if}
        </Tile>
      </div>

      <div class="grid min-w-0 gap-4 sm:grid-cols-2 xl:col-span-2">
        <StatTile icon={Zap} label="Events" value={data.events.toLocaleString()} />
        <StatTile icon={CircleDot} label="New issues" value={data.newIssues.toLocaleString()} footnote={`${data.openIssues.toLocaleString()} open issues`} />
        <div class="min-w-0 sm:col-span-2">
          <Tile icon={HeartPulse}>
            <span slot="title">Crash-free sessions</span>
            {#if sessions && sessions.summary.totalSessions > 0}
              <div class="flex flex-wrap items-baseline gap-x-6 gap-y-1">
                <p class="text-3xl font-extrabold tabular-nums {tierColor(sessions.summary.crashFreeSessions)}">{fmtPct(sessions.summary.crashFreeSessions)}</p>
                <p class="text-sm text-muted-foreground">sessions · <span class="{tierColor(sessions.summary.crashFreeUsers)} font-semibold">{fmtPct(sessions.summary.crashFreeUsers)}</span> users</p>
              </div>
              <p class="mt-1 text-xs text-muted-foreground">{sessions.summary.totalSessions.toLocaleString()} sessions · {sessions.summary.users.toLocaleString()} users</p>
            {:else}
              <p class="py-4 text-center text-sm text-muted-foreground">No sessions yet — enable session tracking in your SDK to see release health.</p>
            {/if}
          </Tile>
        </div>
      </div>

      <div class="min-w-0 xl:col-span-3">
        <Tile icon={Gauge}>
          <span slot="title">Session health</span>
          {#if sessions && sessions.timeseries.length}
            <canvas id="ov-sessions" height="220"></canvas>
          {:else}
            <p class="py-10 text-center text-sm text-muted-foreground">No sessions yet — enable session tracking in your SDK to see release health.</p>
          {/if}
        </Tile>
      </div>

      <div class="min-w-0">
        <Tile icon={Timer}>
          <span slot="title">Latency</span>
          <span slot="subtitle">All-time · needs transaction data, which errgotrack does not collect.</span>
          <p class="py-10 text-center text-sm text-muted-foreground">No transaction data.</p>
        </Tile>
      </div>

      <div class="min-w-0 xl:col-span-4">
        <Tile icon={Trophy}>
          <span slot="title">Top issues</span>
          <span slot="subtitle">Open issues by lifetime event count.</span>
          {#if data.topIssues.length === 0}
            <p class="py-6 text-center text-sm text-muted-foreground">No open issues. Enjoy the quiet.</p>
          {:else}
            <div class="divide-y divide-border">
              {#each data.topIssues as i}
                <button class="flex w-full items-center justify-between gap-3 py-2 text-left" on:click={() => navigate(`/issues/${i.id}`)}>
                  <span class="flex min-w-0 items-center gap-2">
                    <LevelBadge level={i.level} />
                    <span class="truncate text-sm font-medium hover:underline">{i.title}</span>
                  </span>
                  <span class="shrink-0 text-xs text-muted-foreground">{i.eventCount.toLocaleString()} events · {i.lastSeenRelative}</span>
                </button>
              {/each}
            </div>
          {/if}
        </Tile>
      </div>
    </div>
  {/if}
</div>
