<script lang="ts">
  import { Tabs } from "bits-ui";
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    value,
    children,
    ...rest
  }: { class?: string; value: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<Tabs.Content data-slot="tabs-content" {value} class={cn("flex-1 outline-none", className)} {...rest}>
  {@render children?.()}
</Tabs.Content>
