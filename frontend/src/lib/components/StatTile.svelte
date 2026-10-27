<script lang="ts">
  import Tile from "$lib/components/Tile.svelte";
  export let label: string;
  export let value: string | number;
  export let footnote = "";
  export let icon: any = null;
</script>

<Tile {icon}>
  <span slot="title">{label}</span>
  <p class="mt-1 text-3xl font-extrabold tabular-nums">{value}</p>
  {#if footnote}<p class="mt-1 text-xs text-muted-foreground">{footnote}</p>{/if}
</Tile>
