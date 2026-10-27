<script lang="ts">
  import { onMount } from "svelte";
  import { AreaChart } from "layerchart";
  import { Activity, HeartPulse, Timer, Trophy, Zap, CircleDot, Gauge } from "lucide-svelte";
  import { api, type ProjectOverview, type SessionSummary } from "$lib/api";
  import * as Chart from "$lib/components/ui/chart/index.js";
  import Button from "$lib/components/button.svelte";
  import Tile from "$lib/components/Tile.svelte";
  import TileSkeleton from "$lib/components/TileSkeleton.svelte";
  import StatTile from "$lib/components/StatTile.svelte";
  import LevelBadge from "$lib/components/LevelBadge.svelte";
  import RelativeTime from "$lib/components/RelativeTime.svelte";

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

  const severityMeta: Record<string, { color: string; order: number; label: string }> = {
    error: { color: "#ef4444", order: 0, label: "Error" },
    fatal: { color: "#b91c1c", order: 1, label: "Fatal" },
    warning: { color: "#f59e0b", order: 2, label: "Warning" },
    info: { color: "#3b82f6", order: 3, label: "Info" },
    debug: { color: "#9ca3af", order: 4, label: "Debug" },
  };

  function severityLevels(): string[] {
    if (!data) return [];
    const levels = new Set<string>();
    for (const b of data.volumeBySeverity) for (const l of Object.keys(b.counts)) levels.add(l);
    return [...levels].sort((a, b) => {
      const ma = severityMeta[a.toLowerCase()] ?? { order: 5 };
      const mb = severityMeta[b.toLowerCase()] ?? { order: 5 };
      return ma.order - mb.order || a.localeCompare(b);
    });
  }

  function severityRows() {
    if (!data) return [];
    return data.volumeBySeverity.map((b) => ({ bucket: b.bucket, ...b.counts }));
  }

  function severityConfig(): Chart.ChartConfig {
    const cfg: Chart.ChartConfig = {};
    for (const level of severityLevels()) {
      const meta = severityMeta[level.toLowerCase()] ?? { color: "#8b5cf6", label: level };
      cfg[level] = { label: meta.label, color: meta.color };
    }
    return cfg;
  }

  function severitySeries() {
    return severityLevels().map((level) => {
      const meta = severityMeta[level.toLowerCase()] ?? { color: "#8b5cf6", label: level };
      return { key: level, label: meta.label, color: meta.color };
    });
  }

  function sessionRows() {
    if (!sessions) return [];
    return sessions.timeseries.map((p) => ({ bucket: p.bucket, healthy: p.healthy, crashed: p.crashed }));
  }

  const sessionConfig = {
    healthy: { label: "Healthy", color: "#16a34a" },
    crashed: { label: "Crashed", color: "#dc2626" },
  } satisfies Chart.ChartConfig;

  async function load() {
    error = "";
    try {
      const [ov, sess] = await Promise.all([
        api.projectOverview(projectId, period),
        api.projectSessionSummary(projectId, period),
      ]);
      data = ov;
      sessions = sess;
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load overview.";
    }
  }

  onMount(load);
  $: if (period) load();
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
            <Chart.Container config={severityConfig()} class="min-h-[300px]">
              <AreaChart
                data={severityRows()}
                x="bucket"
                series={severitySeries()}
                seriesLayout="stack"
                legend
                props={{ xAxis: { format: (d: string) => d.slice(5) } }}
              >
                {#snippet tooltip()}
                  <Chart.Tooltip labelKey="bucket" />
                {/snippet}
              </AreaChart>
            </Chart.Container>
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
            <Chart.Container config={sessionConfig} class="min-h-[220px]">
              <AreaChart
                data={sessionRows()}
                x="bucket"
                series={[
                  { key: "healthy", label: "Healthy", color: "#16a34a" },
                  { key: "crashed", label: "Crashed", color: "#dc2626" },
                ]}
                seriesLayout="stack"
                legend
              >
                {#snippet tooltip()}
                  <Chart.Tooltip labelKey="bucket" />
                {/snippet}
              </AreaChart>
            </Chart.Container>
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
                  <span class="shrink-0 text-xs text-muted-foreground">{i.eventCount.toLocaleString()} events · <RelativeTime iso={i.lastSeen} /></span>
                </button>
              {/each}
            </div>
          {/if}
        </Tile>
      </div>
    </div>
  {/if}
</div>
