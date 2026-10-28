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

<AlertDialog.Cancel
  data-slot="alert-dialog-cancel"
  class={cn("rounded-md border border-input bg-background px-3 py-1.5 text-sm hover:bg-muted", className)}
  {...rest}
>
  {@render children?.()}
</AlertDialog.Cancel>
