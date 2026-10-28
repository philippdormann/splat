<script lang="ts">
  import { Pagination } from "bits-ui";
  import { cn } from "$lib/utils";
  import { ChevronRight } from "lucide-svelte";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<Pagination.NextButton
  data-slot="pagination-next"
  class={cn("flex items-center gap-1 rounded-md border border-input bg-background px-2.5 py-2 text-sm hover:bg-muted disabled:opacity-50", className)}
  {...rest}
>
  {#if children}{@render children()}{:else}<span>Next</span>{/if}
  <ChevronRight class="size-4" />
</Pagination.NextButton>
