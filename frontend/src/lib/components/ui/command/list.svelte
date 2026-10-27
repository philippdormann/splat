<script lang="ts">
  import { Command } from "bits-ui";
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<Command.List
  class={cn("max-h-[300px] overflow-y-auto overflow-x-hidden", className)}
  {...rest}
>
  {@render children?.()}
</Command.List>
