<script lang="ts">
  import Card from "$lib/components/card.svelte";
  // Static height classes: dynamic pixel values would need an inline
  // style attribute, which our Content-Security-Policy blocks.
  export let height = 140;
  const heights: Record<number, string> = {
    56: "h-14",
    132: "h-32",
    140: "h-36",
    300: "h-72",
  };
  $: cls = heights[height] ?? "h-36";
</script>

<Card cls="h-full p-4">
  <div class="mb-2 h-3 w-28 animate-pulse rounded bg-muted"></div>
  <div class="w-full animate-pulse rounded bg-muted {cls}"></div>
</Card>
