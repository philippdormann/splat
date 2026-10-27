<script lang="ts">
  import { Command } from "bits-ui";
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    heading = "",
    ...rest
  }: { class?: string; children?: Snippet; heading?: string } & Record<string, unknown> = $props();
</script>

<Command.Group
  class={cn("overflow-hidden p-1 text-foreground", className)}
  {...rest}
>
  {#if heading}
    <Command.GroupHeading
      class="px-2 py-1.5 text-xs font-medium text-muted-foreground"
    >
      {heading}
    </Command.GroupHeading>
  {/if}
  <Command.GroupItems>
    {@render children?.()}
  </Command.GroupItems>
</Command.Group>
