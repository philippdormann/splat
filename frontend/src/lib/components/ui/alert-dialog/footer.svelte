<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<div data-slot="alert-dialog-footer" class={cn("mt-5 flex justify-end gap-2", className)} {...rest}>
  {@render children?.()}
</div>
