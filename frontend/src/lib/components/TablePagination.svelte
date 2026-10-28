<script lang="ts">
  import * as Pagination from "$lib/components/ui/pagination/index.js";

  export let currentPage = 1;
  export let totalPages = 1;
  export let totalCount = 0;
  export let perPage = 25;
  export let disabled = false;
  export let onPageChange: (page: number) => void = () => {};
</script>

{#if totalPages > 1}
  <div class="mt-4 flex flex-col items-center gap-2">
    <Pagination.Root
      count={totalCount}
      perPage={perPage}
      page={currentPage}
      onPageChange={(p: number) => { if (!disabled && p !== currentPage) onPageChange(p); }}
    >
      {#snippet children({ pages, currentPage: current }: { pages: { value: number; key: string; type: "page" | "ellipsis" }[]; currentPage: number })}
        <Pagination.Content>
          <Pagination.Item><Pagination.Previous /></Pagination.Item>
          {#each pages as pg (pg.key)}
            {#if pg.type === "ellipsis"}
              <Pagination.Item><Pagination.Ellipsis /></Pagination.Item>
            {:else}
              <Pagination.Item>
                <Pagination.Link page={pg} isActive={current === pg.value}>{pg.value}</Pagination.Link>
              </Pagination.Item>
            {/if}
          {/each}
          <Pagination.Item><Pagination.Next /></Pagination.Item>
        </Pagination.Content>
        <p class="mt-2 text-center text-sm text-muted-foreground tabular-nums">
          Page {currentPage} of {totalPages} · {totalCount} project{totalCount === 1 ? "" : "s"}
        </p>
      {/snippet}
    </Pagination.Root>
  </div>
{:else if totalCount > 0}
  <p class="mt-4 text-center text-sm text-muted-foreground tabular-nums">
    {totalCount} project{totalCount === 1 ? "" : "s"}
  </p>
{/if}
