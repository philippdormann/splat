<script lang="ts">
  import Button from "$lib/components/button.svelte";

  export let open = false;
  export let title = "Are you sure?";
  export let description = "";
  export let confirmLabel = "Confirm";
  export let variant: "destructive" | "default" = "destructive";
  export let busy = false;
  export let onConfirm: () => void = () => {};
  export let onCancel: () => void = () => {};

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape" && !busy) onCancel();
  }
</script>

<svelte:window on:keydown={open ? onKeydown : undefined} />

{#if open}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
    on:click={(e) => {
      if (e.target === e.currentTarget && !busy) onCancel();
    }}
  >
    <div
      class="w-full max-w-md rounded-lg border border-border bg-card p-6 shadow-lg"
      role="alertdialog"
      aria-modal="true"
      aria-label={title}
    >
      <p class="text-base font-semibold">{title}</p>
      {#if description}<p class="mt-2 text-sm text-muted-foreground">{description}</p>{/if}
      {#if $$slots.default}<div class="mt-3"><slot /></div>{/if}
      <div class="mt-5 flex justify-end gap-2">
        <Button size="sm" variant="outline" disabled={busy} on:click={onCancel}>Cancel</Button>
        <Button size="sm" variant={variant === "destructive" ? "destructive" : "default"} disabled={busy} on:click={onConfirm}>
          {busy ? "Working…" : confirmLabel}
        </Button>
      </div>
    </div>
  </div>
{/if}
