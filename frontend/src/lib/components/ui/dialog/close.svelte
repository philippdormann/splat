<script lang="ts">
  import { Dialog } from "bits-ui";
  import { X } from "lucide-svelte";
  import { cn } from "$lib/utils";

  let {
    class: className = "",
    ...rest
  }: { class?: string } & Record<string, unknown> = $props();
</script>

<Dialog.Close
  data-slot="dialog-close"
  class={cn("rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted", className)}
  aria-label="Close dialog"
  {...rest}
>
  <X class="h-4 w-4" />
</Dialog.Close>
