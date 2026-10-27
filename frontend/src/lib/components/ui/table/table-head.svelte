<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<th
  data-slot="table-head"
  class={cn("text-muted-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap", className)}
  {...rest}
>
  {@render children?.()}
</th>
