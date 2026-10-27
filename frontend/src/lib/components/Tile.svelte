<script lang="ts">
  import Card from "$lib/components/card.svelte";
  export let icon: any = null;
</script>

<Card cls="h-full p-4">
  <div class="mb-1 flex items-center gap-2">
    {#if icon}<svelte:component this={icon} class="h-4 w-4 text-muted-foreground" />{/if}
    <p class="text-xs font-bold uppercase tracking-widest text-muted-foreground"><slot name="title" /></p>
  </div>
  {#if $$slots.subtitle}<p class="mb-2 text-xs text-muted-foreground"><slot name="subtitle" /></p>{/if}
  <slot />
</Card>
