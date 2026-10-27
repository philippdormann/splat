<script lang="ts">
  import { DropdownMenu } from "bits-ui";
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    onclick,
    ...rest
  }: { class?: string; children?: Snippet; onclick?: (e: MouseEvent) => void } & Record<string, unknown> = $props();
</script>

<DropdownMenu.Trigger
  class={cn(
    "flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0",
    className,
  )}
  {onclick}
  {...rest}
>
  {@render children?.()}
</DropdownMenu.Trigger>
