import { getContext, setContext } from "svelte";

export type ChartConfig = Record<
  string,
  {
    label?: string;
    color?: string;
    theme?: { light: string; dark: string };
  }
>;

const CHART_CONTEXT_KEY = Symbol("errgotrack-chart-config");

export function setChartContext(config: ChartConfig): void {
  setContext(CHART_CONTEXT_KEY, config);
}

export function getChartContext(): ChartConfig {
  return getContext<ChartConfig>(CHART_CONTEXT_KEY) ?? {};
}
