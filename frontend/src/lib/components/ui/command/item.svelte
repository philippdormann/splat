<script lang="ts">
  import { Command } from "bits-ui";
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    onSelect,
    ...rest
  }: {
    class?: string;
    children?: Snippet;
    onSelect?: () => void;
    value?: string;
    keywords?: string[];
    disabled?: boolean;
  } & Record<string, unknown> = $props();
</script>

<Command.Item
  class={cn(
    "relative flex cursor-pointer select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none",
    "data-[selected]:bg-muted data-[selected]:text-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
    "[&_svg]:size-4 [&_svg]:shrink-0",
    className,
  )}
  {onSelect}
  {...rest}
>
  {@render children?.()}
</Command.Item>
