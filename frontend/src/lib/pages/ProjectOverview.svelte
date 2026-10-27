<script lang="ts">
  import { onMount } from "svelte";
  import { AreaChart } from "layerchart";
  import { Activity, HeartPulse, Timer, Trophy, Zap, CircleDot, Gauge, TriangleAlert } from "lucide-svelte";
  import { api, type ProjectOverview, type SessionSummary } from "$lib/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Tabs from "$lib/components/ui/tabs/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import { Root as Skeleton } from "$lib/components/ui/skeleton/index.js";
  import * as Chart from "$lib/components/ui/chart/index.js";
  import Button from "$lib/components/button.svelte";
  import LevelBadge from "$lib/components/LevelBadge.svelte";
  import RelativeTime from "$lib/components/RelativeTime.svelte";

  export let projectId: string;
  export let navigate: (path: string) => void;

  const periods = [
    { value: "24h", label: "24H" },
    { value: "7d", label: "7D" },
    { value: "30d", label: "30D" },
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
    <Tabs.Root bind:value={period}>
      <Tabs.List aria-label="Period">
        {#each periods as p}
          <Tabs.Trigger value={p.value}>{p.label}</Tabs.Trigger>
        {/each}
      </Tabs.List>
    </Tabs.Root>
  </div>

  {#if error}
    <Alert.Root variant="destructive">
      <TriangleAlert />
      <Alert.Title>Couldn't load the overview</Alert.Title>
      <Alert.Description>{error}</Alert.Description>
    </Alert.Root>
  {:else if !data}
    <div class="grid gap-4 xl:grid-cols-4" aria-label="Loading overview">
      <div class="min-w-0 xl:col-span-2">
        <Card.Root><Card.Content class="pt-6"><Skeleton class="h-72 w-full" /></Card.Content></Card.Root>
      </div>
      <div class="grid min-w-0 gap-4 sm:grid-cols-2 xl:col-span-2">
        <Card.Root><Card.Content class="pt-6"><Skeleton class="h-14 w-full" /></Card.Content></Card.Root>
        <Card.Root><Card.Content class="pt-6"><Skeleton class="h-14 w-full" /></Card.Content></Card.Root>
        <div class="min-w-0 sm:col-span-2">
          <Card.Root><Card.Content class="pt-6"><Skeleton class="h-32 w-full" /></Card.Content></Card.Root>
        </div>
      </div>
    </div>
  {:else}
    <div class="grid gap-4 xl:grid-cols-4">
      <div class="min-w-0 xl:col-span-2">
        <Card.Root class="h-full">
          <Card.Header class="flex-row items-center gap-2">
            <Activity class="text-muted-foreground size-4" />
            <Card.Title class="text-sm">Error volume by severity</Card.Title>
          </Card.Header>
          <Card.Content>
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
          </Card.Content>
        </Card.Root>
      </div>

      <div class="grid min-w-0 gap-4 sm:grid-cols-2 xl:col-span-2">
        <Card.Root class="gap-0">
          <Card.Header class="flex-row items-center gap-2 pb-2">
            <Zap class="text-muted-foreground size-4" />
            <Card.Description>Events</Card.Description>
          </Card.Header>
          <Card.Content>
            <p class="text-3xl font-semibold tracking-tight tabular-nums">{data.events.toLocaleString()}</p>
          </Card.Content>
        </Card.Root>
        <Card.Root class="gap-0">
          <Card.Header class="flex-row items-center gap-2 pb-2">
            <CircleDot class="text-muted-foreground size-4" />
            <Card.Description>New issues</Card.Description>
          </Card.Header>
          <Card.Content>
            <p class="text-3xl font-semibold tracking-tight tabular-nums">{data.newIssues.toLocaleString()}</p>
          </Card.Content>
          <Card.Footer class="pt-2">
            <span class="text-muted-foreground text-xs">{data.openIssues.toLocaleString()} open issues</span>
          </Card.Footer>
        </Card.Root>
        <div class="min-w-0 sm:col-span-2">
          <Card.Root class="h-full">
            <Card.Header class="flex-row items-center gap-2">
              <HeartPulse class="text-muted-foreground size-4" />
              <Card.Title class="text-sm">Crash-free sessions</Card.Title>
            </Card.Header>
            <Card.Content>
              {#if sessions && sessions.summary.totalSessions > 0}
                <div class="flex flex-wrap items-baseline gap-x-6 gap-y-1">
                  <p class="text-3xl font-semibold tracking-tight tabular-nums {tierColor(sessions.summary.crashFreeSessions)}">{fmtPct(sessions.summary.crashFreeSessions)}</p>
                  <p class="text-sm text-muted-foreground">sessions · <span class="{tierColor(sessions.summary.crashFreeUsers)} font-semibold">{fmtPct(sessions.summary.crashFreeUsers)}</span> users</p>
                </div>
                <p class="mt-1 text-xs text-muted-foreground">{sessions.summary.totalSessions.toLocaleString()} sessions · {sessions.summary.users.toLocaleString()} users</p>
              {:else}
                <p class="py-4 text-center text-sm text-muted-foreground">No sessions yet — enable session tracking in your SDK to see release health.</p>
              {/if}
            </Card.Content>
          </Card.Root>
        </div>
      </div>

      <div class="min-w-0 xl:col-span-3">
        <Card.Root class="h-full">
          <Card.Header class="flex-row items-center gap-2">
            <Gauge class="text-muted-foreground size-4" />
            <Card.Title class="text-sm">Session health</Card.Title>
          </Card.Header>
          <Card.Content>
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
          </Card.Content>
        </Card.Root>
      </div>

      <div class="min-w-0">
        <Card.Root class="h-full">
          <Card.Header>
            <div class="flex items-center gap-2">
              <Timer class="text-muted-foreground size-4" />
              <Card.Title class="text-sm">Latency</Card.Title>
            </div>
            <Card.Description>Percentiles need sampled APM data, which Splat does not collect. Stored transactions live under Traces.</Card.Description>
          </Card.Header>
          <Card.Content>
            <p class="py-10 text-center text-sm text-muted-foreground">No transaction data.</p>
          </Card.Content>
        </Card.Root>
      </div>

      <div class="min-w-0 xl:col-span-4">
        <Card.Root class="gap-0">
          <Card.Header>
            <div class="flex items-center gap-2">
              <Trophy class="text-muted-foreground size-4" />
              <Card.Title class="text-sm">Top issues</Card.Title>
            </div>
            <Card.Description>Open issues by lifetime event count</Card.Description>
          </Card.Header>
          <Card.Content>
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
                    <span class="shrink-0 text-xs text-muted-foreground tabular-nums">{i.eventCount.toLocaleString()} events · <RelativeTime iso={i.lastSeen} /></span>
                  </button>
                {/each}
              </div>
            {/if}
          </Card.Content>
        </Card.Root>
      </div>
    </div>
  {/if}
</div>
