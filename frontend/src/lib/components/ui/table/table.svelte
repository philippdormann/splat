<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<div data-slot="table-container" class={cn("relative w-full overflow-x-auto", className)} {...rest}>
  <table data-slot="table" class="w-full caption-bottom text-sm">
    {@render children?.()}
  </table>
</div>
