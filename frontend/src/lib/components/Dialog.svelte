<script lang="ts">
  import { X } from "lucide-svelte";

  export let open = false;
  export let title = "";
  export let onClose: () => void = () => {};

  let panel: HTMLElement | null = null;

  const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

  function focusables(): HTMLElement[] {
    if (!panel) return [];
    return Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
      (el) => el.offsetParent !== null,
    );
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      onClose();
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

  $: if (open && typeof document !== "undefined") {
    queueMicrotask(() => {
      focusables()[0]?.focus();
    });
  }
</script>

<svelte:window on:keydown={open ? onKeydown : undefined} />

{#if open}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
    on:click={(e) => {
      if (e.target === e.currentTarget) onClose();
    }}
  >
    <div
      bind:this={panel}
      class="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-lg border border-border bg-card p-6 shadow-lg"
      role="dialog"
      aria-modal="true"
      aria-label={title}
    >
      <div class="mb-4 flex items-start justify-between gap-2">
        <p class="text-base font-semibold">{title}</p>
        <button class="rounded-md p-1 text-muted-foreground hover:bg-muted" on:click={onClose} aria-label="Close dialog"><X class="h-4 w-4" /></button>
      </div>
      <slot />
    </div>
  </div>
{/if}
