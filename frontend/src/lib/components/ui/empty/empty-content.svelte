<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<div
  data-slot="empty-content"
  class={cn("flex w-full max-w-sm min-w-0 flex-col items-center gap-1 text-sm text-balance", className)}
  {...rest}
>
  {@render children?.()}
</div>
