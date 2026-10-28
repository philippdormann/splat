<script lang="ts">
  import { Pagination } from "bits-ui";
  import { cn } from "$lib/utils";

  let {
    count,
    perPage = 25,
    page = $bindable(1),
    onPageChange,
    class: className = "",
    ...rest
  }: {
    count: number;
    perPage?: number;
    page?: number;
    onPageChange?: (page: number) => void;
    class?: string;
  } & Record<string, unknown> = $props();
</script>

<Pagination.Root {count} {perPage} bind:page {onPageChange} class={cn("mx-auto flex w-full justify-center", className)} {...rest} />
