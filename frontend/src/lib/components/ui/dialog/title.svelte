<script lang="ts">
  import { Dialog } from "bits-ui";
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<Dialog.Title data-slot="dialog-title" class={cn("text-base font-semibold", className)} {...rest}>
  {@render children?.()}
</Dialog.Title>
