<script lang="ts">
  // Tiny event-trend sparkline (Rustrak-style TrendSparkline): 12 bars,
  // health-colored, no axes. minScale keeps a lone issue from drawing a
  // full-height bar identical to a meltdown.
  export let trend: number[] = [];
  export let health: "critical" | "rising" | "quiet" = "quiet";
  export let label = "";

  const barClass =
    health === "critical"
      ? "fill-red-500/80 dark:fill-red-400/80"
      : health === "rising"
        ? "fill-amber-500/80 dark:fill-amber-400/80"
        : "fill-muted-foreground/40";

  const minScale = 4;
  $: max = Math.max(minScale, ...trend);
</script>

<svg viewBox={`0 0 ${Math.max(1, trend.length) * 10} 36`} class="h-9 w-full" role="img" aria-label={label}>
  {#each trend as v, i}
    {@const h = Math.max(2, Math.round((v / max) * 34))}
    <rect x={i * 10 + 2} y={36 - h} width="6" height={h} rx="1" class={barClass} />
  {/each}
</svg>
