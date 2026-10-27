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
  data-slot="empty"
  class={cn(
    "flex min-w-0 flex-1 flex-col items-center gap-6 rounded-lg border-dashed p-6 text-center text-balance border",
    className,
  )}
  {...rest}
>
  {@render children?.()}
</div>
