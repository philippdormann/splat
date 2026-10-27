<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<thead data-slot="table-header" class={cn("[&_tr]:border-b", className)} {...rest}>
  {@render children?.()}
</thead>
