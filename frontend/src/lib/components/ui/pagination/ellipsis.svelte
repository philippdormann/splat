<script lang="ts">
  import { cn } from "$lib/utils";
  import { Ellipsis } from "lucide-svelte";

  let { class: className = "", ...rest }: { class?: string } & Record<string, unknown> = $props();
</script>

<span data-slot="pagination-ellipsis" aria-hidden class={cn("flex size-9 items-center justify-center", className)} {...rest}>
  <Ellipsis class="size-4" />
  <span class="sr-only">More pages</span>
</span>
