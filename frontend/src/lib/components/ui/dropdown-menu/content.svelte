<script lang="ts">
  import { DropdownMenu } from "bits-ui";
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<DropdownMenu.Portal>
  <DropdownMenu.Content
    class={cn(
      "z-50 min-w-52 overflow-hidden rounded-md border border-border bg-card p-1 text-card-foreground shadow-md",
      className,
    )}
    {...rest}
  >
    {@render children?.()}
  </DropdownMenu.Content>
</DropdownMenu.Portal>
