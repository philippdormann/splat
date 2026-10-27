<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<td data-slot="table-cell" class={cn("p-2 align-middle", className)} {...rest}>
  {@render children?.()}
</td>
