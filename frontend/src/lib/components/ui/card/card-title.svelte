<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<div data-slot="card-title" class={cn("leading-none font-semibold", className)} {...rest}>
  {@render children?.()}
</div>
