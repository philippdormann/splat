<script lang="ts">
  import { DropdownMenu } from "bits-ui";
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
  } & Record<string, unknown> = $props();
</script>

<DropdownMenu.Item
  class={cn(
    "relative flex cursor-pointer select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none transition-colors",
    "hover:bg-muted focus:bg-muted data-[highlighted]:bg-muted",
    "[&_svg]:size-4 [&_svg]:shrink-0",
    className,
  )}
  {onSelect}
  {...rest}
>
  {@render children?.()}
</DropdownMenu.Item>
