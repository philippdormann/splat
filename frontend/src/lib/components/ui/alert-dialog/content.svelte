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

<AlertDialog.Portal>
  <AlertDialog.Overlay data-slot="alert-dialog-overlay" class="fixed inset-0 z-50 bg-black/50" />
  <AlertDialog.Content
    data-slot="alert-dialog-content"
    class={cn("fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-card p-6 shadow-lg", className)}
    {...rest}
  >
    {@render children?.()}
  </AlertDialog.Content>
</AlertDialog.Portal>
