<script lang="ts">
  import { Pagination } from "bits-ui";
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  // Ellipsis rows never reach this component (the table renders
  // Pagination.Ellipsis for those); narrow the union for bits.

  let {
    page,
    class: className = "",
    isActive = false,
    children,
    ...rest
  }: {
    page: { value: number; key: string; type: string };
    class?: string;
    isActive?: boolean;
    children?: Snippet;
  } & Record<string, unknown> = $props();
</script>

<Pagination.Page
  page={{ ...page, type: "page" as const }}
  class={cn(
    "flex size-9 items-center justify-center rounded-md border text-sm transition-colors",
    isActive
      ? "border-primary bg-primary text-primary-foreground"
      : "border-input bg-background hover:bg-muted",
    className,
  )}
  {...rest}
>
  {#if children}{@render children()}{:else}{page.value}{/if}
</Pagination.Page>
