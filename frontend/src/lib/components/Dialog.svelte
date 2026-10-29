<script lang="ts">
  import * as DialogUI from "$lib/components/ui/dialog/index.js";

  export let open = false;
  export let title = "";
  export let onClose: () => void = () => {};

  // bits-ui owns Esc, overlay-outside, focus trap, scroll lock and focus
  // restore; we only translate "closed" back into the parent's onClose.
  function handleOpenChange(o: boolean) {
    if (!o) onClose();
  }
</script>

<DialogUI.Root {open} onOpenChange={handleOpenChange}>
  <DialogUI.Content class="max-h-[90vh] overflow-y-auto sm:max-w-lg">
    <div class="mb-4 flex items-start justify-between gap-2">
      <DialogUI.Title>{title}</DialogUI.Title>
      <DialogUI.Close />
    </div>
    <slot />
  </DialogUI.Content>
</DialogUI.Root>
