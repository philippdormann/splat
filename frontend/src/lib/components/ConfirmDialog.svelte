<script lang="ts">
  import * as DialogUI from "$lib/components/ui/dialog/index.js";
  import Button from "$lib/components/button.svelte";

  export let open = false;
  export let title = "Are you sure?";
  export let description = "";
  export let confirmLabel = "Confirm";
  export let variant: "destructive" | "default" = "destructive";
  export let busy = false;
  export let onConfirm: () => void = () => {};
  export let onCancel: () => void = () => {};

  // Dismissals (Esc, overlay-outside) are swallowed while busy so a
  // pending mutation can't be orphaned behind a closed dialog.
  function handleOpenChange(o: boolean) {
    if (!o && !busy) onCancel();
  }
</script>

<DialogUI.Root {open} onOpenChange={handleOpenChange}>
  <DialogUI.Content>
    <DialogUI.Header>
      <DialogUI.Title>{title}</DialogUI.Title>
      {#if description}<DialogUI.Description>{description}</DialogUI.Description>{/if}
    </DialogUI.Header>
    {#if $$slots.default}<div class="mt-3"><slot /></div>{/if}
    <DialogUI.Footer class="mt-5 sm:justify-end">
      <Button size="sm" variant="outline" disabled={busy} on:click={onCancel}>Cancel</Button>
      <Button size="sm" variant={variant === "destructive" ? "destructive" : "default"} disabled={busy} on:click={onConfirm}>
        {busy ? "Working…" : confirmLabel}
      </Button>
    </DialogUI.Footer>
  </DialogUI.Content>
</DialogUI.Root>
