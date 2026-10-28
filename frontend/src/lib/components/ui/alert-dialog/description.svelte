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

<AlertDialog.Description data-slot="alert-dialog-description" class={cn("text-sm text-muted-foreground", className)} {...rest}>
  {@render children?.()}
</AlertDialog.Description>
