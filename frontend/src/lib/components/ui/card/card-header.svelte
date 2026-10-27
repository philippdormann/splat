<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<div data-slot="card-header" class={cn("flex flex-col gap-1.5 px-6", className)} {...rest}>
  {@render children?.()}
</div>
