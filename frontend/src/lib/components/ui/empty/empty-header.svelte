<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<div data-slot="empty-header" class={cn("flex max-w-sm flex-col items-center gap-2", className)} {...rest}>
  {@render children?.()}
</div>
