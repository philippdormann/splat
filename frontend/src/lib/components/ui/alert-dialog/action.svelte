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

<AlertDialog.Action
  data-slot="alert-dialog-action"
  class={cn("rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground hover:bg-primary/90", className)}
  {...rest}
>
  {@render children?.()}
</AlertDialog.Action>
