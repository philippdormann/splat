<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    onclick,
    ...rest
  }: { class?: string; children?: Snippet; onclick?: (e: MouseEvent) => void } & Record<string, unknown> =
    $props();
</script>

<tr
  data-slot="table-row"
  class={cn("hover:bg-muted/50 data-[state=selected]:bg-muted border-b transition-colors", className)}
  {onclick}
  {...rest}
>
  {@render children?.()}
</tr>
