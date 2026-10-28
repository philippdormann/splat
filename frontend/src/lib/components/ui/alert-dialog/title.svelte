<script lang="ts">
  import { AlertDialog } from "bits-ui";
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<AlertDialog.Title data-slot="alert-dialog-title" class={cn("text-base font-semibold", className)} {...rest}>
  {@render children?.()}
</AlertDialog.Title>
