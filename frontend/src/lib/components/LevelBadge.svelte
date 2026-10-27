<script lang="ts">
  import { OctagonX, TriangleAlert, Info } from "lucide-svelte";
  import Badge from "$lib/components/badge.svelte";

  export let level: string;

  const map: Record<string, { variant: "destructive" | "secondary" | "outline"; icon: typeof Info; cls: string }> = {
    error: { variant: "destructive", icon: OctagonX, cls: "" },
    fatal: { variant: "destructive", icon: OctagonX, cls: "" },
    warning: { variant: "outline", icon: TriangleAlert, cls: "border-amber-500 text-amber-600 dark:text-amber-400" },
    info: { variant: "secondary", icon: Info, cls: "" },
    debug: { variant: "secondary", icon: Info, cls: "" },
  };
  $: entry = map[(level ?? "").toLowerCase()] ?? { variant: "secondary" as const, icon: Info, cls: "" };
</script>

<Badge variant={entry.variant} class={entry.cls}>
  <svelte:component this={entry.icon} class="h-3 w-3" />
  {level}
</Badge>
