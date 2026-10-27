<script lang="ts">
  import { Tooltip, getChartContext } from "layerchart";
  import { cn } from "$lib/utils";
  import { getChartContext as getConfig } from "./chart-utils.js";

  let {
    class: className = "",
    labelKey,
    hideLabel = false,
    ...rest
  }: {
    class?: string;
    labelKey?: string;
    hideLabel?: boolean;
  } & Record<string, unknown> = $props();

  const chart = getChartContext();
  const config = getConfig();

  type Row = { label: string; value: number; color?: string };

  function rowsFor(datum: any): Row[] {
    if (datum == null || typeof datum !== "object") return [];
    const series = (chart.tooltip.series ?? []).filter((s: any) => s.visible !== false);
    const out: Row[] = [];
    for (const s of series) {
      const key = s.config?.key ?? s.key;
      const value = datum[key];
      if (typeof value !== "number") continue;
      out.push({
        label: s.label ?? config[key]?.label ?? key,
        value,
        color: s.color ?? config[key]?.color,
      });
    }
    return out;
  }

  function headerFor(datum: any): string {
    if (datum == null || typeof datum !== "object") return "";
    if (labelKey && datum[labelKey] != null) return String(datum[labelKey]);
    for (const k of ["bucket", "label", "month", "date", "name"]) {
      if (datum[k] != null) return String(datum[k]);
    }
    return "";
  }
</script>

<Tooltip.Root {...rest}>
  {#snippet children({ data }: { data: any })}
    {@const rows = rowsFor(data)}
    {#if rows.length}
      <div class={cn("min-w-32 rounded-md border border-border bg-card px-3 py-2 text-xs shadow-md", className)}>
        {#if !hideLabel}
          {@const header = headerFor(data)}
          {#if header}
            <p class="mb-1 font-semibold">{header}</p>
          {/if}
        {/if}
        <div class="space-y-1">
          {#each rows as row (row.label)}
            <div class="flex items-center gap-2">
              {#if row.color}
                <svg class="size-2.5 shrink-0 rounded-[2px]" viewBox="0 0 10 10" aria-hidden="true"><rect width="10" height="10" rx="2" fill={row.color} /></svg>
              {/if}
              <span class="text-muted-foreground">{row.label}</span>
              <span class="ml-auto pl-4 font-mono font-semibold tabular-nums">{row.value.toLocaleString()}</span>
            </div>
          {/each}
        </div>
      </div>
    {/if}
  {/snippet}
</Tooltip.Root>
