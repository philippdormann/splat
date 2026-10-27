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

  let panel: HTMLElement | null = null;
  let previouslyFocused: Element | null = null;

  const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

  function focusables(): HTMLElement[] {
    if (!panel) return [];
    return Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
      (el) => el.offsetParent !== null,
    );
  }

  // Focus trap: keep Tab inside the dialog, focus the confirm control on
  // open, and hand focus back to whatever opened it on close.
  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape" && !busy) {
      onCancel();
      return;
    }
    if (e.key !== "Tab" || !panel) return;
    const items = focusables();
    if (items.length === 0) {
      e.preventDefault();
      return;
    }
    const first = items[0];
    const last = items[items.length - 1];
    const active = document.activeElement;
    if (e.shiftKey && (active === first || !panel.contains(active))) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
  }

  // `open` is driven by the parent, so react to it rather than mount: the
  // component instance outlives any single opening.
  $: if (open && typeof document !== "undefined") {
    previouslyFocused = document.activeElement;
    queueMicrotask(() => {
      const items = focusables();
      // Prefer the confirm control (last button); fall back to first.
      const target = items[items.length - 1] ?? items[0];
      target?.focus();
    });
  }
  $: if (!open && previouslyFocused instanceof HTMLElement) {
    const el = previouslyFocused;
    previouslyFocused = null;
    queueMicrotask(() => el.focus());
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
      bind:this={panel}
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
