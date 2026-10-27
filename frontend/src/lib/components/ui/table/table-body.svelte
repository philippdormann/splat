<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<tbody data-slot="table-body" class={cn("[&_tr:last-child]:border-0", className)} {...rest}>
  {@render children?.()}
</tbody>
