<script lang="ts">
  import { ChevronRight } from "lucide-svelte";

  export let navigate: (path: string) => void;
  export let items: { label: string; href?: string }[] = [];
</script>

<nav aria-label="Breadcrumb">
  <ol class="flex flex-wrap items-center gap-1 text-sm">
    {#each items as item, idx}
      {@const last = idx === items.length - 1}
      <li class="flex min-w-0 items-center gap-1">
        {#if idx > 0}<ChevronRight class="text-muted-foreground size-3.5 shrink-0" />{/if}
        {#if item.href && !last}
          <button
            class="text-muted-foreground max-w-64 truncate transition-colors hover:text-foreground hover:underline"
            on:click={() => item.href && navigate(item.href)}
          >{item.label}</button>
        {:else}
          <span class="max-w-64 truncate {last ? 'text-foreground font-medium' : 'text-muted-foreground'}" aria-current={last ? "page" : undefined}>{item.label}</span>
        {/if}
      </li>
    {/each}
  </ol>
</nav>
