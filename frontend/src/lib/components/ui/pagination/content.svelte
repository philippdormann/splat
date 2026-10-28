<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<ul data-slot="pagination-content" class={cn("flex flex-row items-center gap-1", className)} {...rest}>
  {@render children?.()}
</ul>
