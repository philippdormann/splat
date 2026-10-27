<script lang="ts">
  import { Tabs } from "bits-ui";
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    value = $bindable(""),
    children,
    ...rest
  }: { class?: string; value?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<Tabs.Root bind:value data-slot="tabs" class={cn("flex flex-col gap-2", className)} {...rest}>
  {@render children?.()}
</Tabs.Root>
