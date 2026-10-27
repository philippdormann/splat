<script lang="ts">
  import { cn } from "$lib/utils";
  import { setChartContext, type ChartConfig } from "./chart-utils.js";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    config = {},
    ...rest
  }: {
    class?: string;
    children?: Snippet;
    config?: ChartConfig;
  } & Record<string, unknown> = $props();

  setChartContext(config);
</script>

<!--
  shadcn-svelte chart container styling, minus ChartStyle: that component
  injects a runtime <style> element, which our Content-Security-Policy
  blocks. Series colors are passed directly in each chart's series config
  instead, so nothing needs runtime CSS.
-->
<div
  data-slot="chart"
  class={cn(
    "flex aspect-video w-full justify-center overflow-visible text-xs",
    "[&_.lc-highlight-point]:stroke-transparent",
    "[&_.lc-line]:stroke-border/50",
    "[&_.lc-highlight-line]:stroke-0",
    "[&_.lc-area-path]:opacity-100 [&_.lc-highlight-line]:opacity-100 [&_.lc-highlight-point]:opacity-100 [&_.lc-spline-path]:opacity-100 [&_.lc-text]:text-xs [&_.lc-text-svg]:overflow-visible",
    "[&_.lc-axis-tick]:stroke-0",
    "[&_.lc-rule-x-line:not(.lc-grid-x-rule)]:stroke-0 [&_.lc-rule-y-line:not(.lc-grid-y-rule)]:stroke-0",
    "[&_.lc-grid-x-radial-circle]:stroke-border [&_.lc-grid-x-radial-line]:stroke-border",
    "[&_.lc-grid-y-radial-circle]:stroke-border [&_.lc-grid-y-radial-line]:stroke-border",
    "[&_.lc-legend-swatch-button]:items-center [&_.lc-legend-swatch-button]:gap-1.5",
    "[&_.lc-legend-swatch-group]:items-center [&_.lc-legend-swatch-group]:gap-4",
    "[&_.lc-legend-swatch]:size-2.5 [&_.lc-legend-swatch]:rounded-[2px]",
    "[&_.lc-labels-text:not([fill])]:fill-foreground [&_text]:stroke-transparent",
    "[&_.lc-axis-tick-label]:fill-muted-foreground [&_.lc-axis-tick-label]:font-normal",
    "[&_.lc-tooltip-rects-g]:fill-transparent",
    "[&_.lc-layout-svg-g]:fill-transparent",
    "[&_.lc-root-container]:w-full",
    className,
  )}
  {...rest}
>
  {@render children?.()}
</div>
