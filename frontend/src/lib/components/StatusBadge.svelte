<script lang="ts">
  import { TriangleAlert, CircleCheck, EyeOff, BellOff } from "lucide-svelte";
  import Badge from "$lib/components/badge.svelte";

  export let status: string;

  // Distinct color + icon per status so state is scannable at a glance.
  const map: Record<string, { variant: "destructive" | "secondary" | "outline"; icon: typeof BellOff; cls: string; label: string }> = {
    unresolved: { variant: "outline", icon: TriangleAlert, cls: "border-amber-500 text-amber-600 dark:text-amber-400", label: "Unresolved" },
    resolved: { variant: "outline", icon: CircleCheck, cls: "border-green-500 text-green-600 dark:text-green-400", label: "Resolved" },
    ignored: { variant: "secondary", icon: EyeOff, cls: "", label: "Ignored" },
    muted: { variant: "secondary", icon: BellOff, cls: "", label: "Muted" },
  };
  $: entry = map[(status ?? "").toLowerCase()] ?? { variant: "secondary" as const, icon: BellOff, cls: "", label: status };
</script>

<Badge variant={entry.variant} class={entry.cls}>
  <svelte:component this={entry.icon} class="h-3 w-3" />
  {entry.label}
</Badge>
