<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<div data-slot="dialog-footer" class={cn("flex flex-col-reverse gap-2 sm:flex-row sm:justify-end", className)} {...rest}>
  {@render children?.()}
</div>
