<script lang="ts">
  import type { Platform } from "$lib/api";
  import PlatformIcon from "$lib/components/PlatformIcon.svelte";
  import { cn } from "$lib/utils";

  export let platforms: Platform[] = [];
  export let value = "";
  export let disabled = false;
  export let onSelect: (id: string) => void = () => {};
</script>

<div class="grid grid-cols-2 gap-2 sm:grid-cols-3" role="radiogroup" aria-label="Platform">
  {#each platforms as p}
    <button
      type="button"
      role="radio"
      aria-checked={value === p.id}
      {disabled}
      on:click={() => onSelect(p.id)}
      class={cn(
        "flex flex-col items-center gap-2 rounded-lg border p-4 text-center transition-colors",
        "hover:border-primary hover:bg-muted",
        "disabled:cursor-not-allowed disabled:opacity-50",
        value === p.id ? "border-primary bg-muted ring-1 ring-primary" : "border-border bg-card",
      )}
    >
      <PlatformIcon platform={p.id} size={28} />
      <span class="text-xs font-medium leading-tight">{p.label}</span>
    </button>
  {/each}
</div>
