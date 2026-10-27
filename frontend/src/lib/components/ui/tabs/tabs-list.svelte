<script lang="ts">
  import { Tabs } from "bits-ui";
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<Tabs.List
  data-slot="tabs-list"
  class={cn("bg-muted text-muted-foreground inline-flex h-9 w-fit items-center justify-center rounded-lg p-[3px]", className)}
  {...rest}
>
  {@render children?.()}
</Tabs.List>
