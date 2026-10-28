<script lang="ts">
  import { Pagination } from "bits-ui";
  import { cn } from "$lib/utils";
  import { ChevronLeft } from "lucide-svelte";
  import type { Snippet } from "svelte";

  let {
    class: className = "",
    children,
    ...rest
  }: { class?: string; children?: Snippet } & Record<string, unknown> = $props();
</script>

<Pagination.PrevButton
  data-slot="pagination-previous"
  class={cn("flex items-center gap-1 rounded-md border border-input bg-background px-2.5 py-2 text-sm hover:bg-muted disabled:opacity-50", className)}
  {...rest}
>
  <ChevronLeft class="size-4" />
  {#if children}{@render children()}{:else}<span>Previous</span>{/if}
</Pagination.PrevButton>
