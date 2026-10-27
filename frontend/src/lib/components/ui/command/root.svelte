<script lang="ts">
  import { Command } from "bits-ui";
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: {
    class?: string;
    children?: Snippet;
    loop?: boolean;
    label?: string;
  } & Record<string, unknown> = $props();
</script>

<Command.Root
  class={cn(
    "flex h-full w-full flex-col overflow-hidden rounded-md bg-card text-card-foreground",
    className,
  )}
  {...rest}
>
  {@render children?.()}
</Command.Root>
