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

<Command.Empty class={cn("py-6 text-center text-sm text-muted-foreground", className)} {...rest}>
  {@render children?.()}
</Command.Empty>
