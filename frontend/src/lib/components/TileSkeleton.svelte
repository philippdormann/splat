<script lang="ts">
  import Card from "$lib/components/card.svelte";
  export let height = 140;
</script>

<Card cls="h-full p-4">
  <div class="mb-2 h-3 w-28 animate-pulse rounded bg-muted" />
  <div class="w-full animate-pulse rounded bg-muted" style="height: {height}px" />
</Card>
