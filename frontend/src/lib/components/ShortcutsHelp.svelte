<script lang="ts">
  import { Zap } from "lucide-svelte";

  let { open = $bindable(false) }: { open?: boolean } = $props();

  const groups: { heading: string; keys: { combo: string; desc: string }[] }[] = [
    {
      heading: "Navigation",
      keys: [
        { combo: "⌘K / Ctrl+K", desc: "Command palette" },
        { combo: "G then D", desc: "Dashboard" },
        { combo: "G then I", desc: "Issues" },
        { combo: "G then T", desc: "Triage queue" },
        { combo: "G then P", desc: "Projects" },
        { combo: "G then S", desc: "Settings" },
        { combo: "?", desc: "This help" },
        { combo: "Esc", desc: "Close dialog" },
      ],
    },
    {
      heading: "Context",
      keys: [
        { combo: "⌘K …", desc: "Page actions (resolve, mute, …) appear at the top of the palette when relevant" },
      ],
    },
  ];
</script>

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div
    class="fixed inset-0 z-50 flex items-start justify-center bg-black/50 p-4 pt-[15vh]"
    onclick={(e) => {
      if (e.target === e.currentTarget) open = false;
    }}
  >
    <div class="w-full max-w-md rounded-lg border border-border bg-card p-4 shadow-lg" role="dialog" aria-label="Keyboard shortcuts">
      <div class="mb-3 flex items-center gap-2">
        <Zap class="size-4 text-muted-foreground" />
        <p class="text-sm font-semibold">Keyboard shortcuts</p>
      </div>
      {#each groups as g}
        <p class="text-muted-foreground mt-3 mb-1 text-xs font-medium first:mt-0">{g.heading}</p>
        <div class="space-y-1">
          {#each g.keys as k}
            <p class="flex items-center justify-between gap-3 text-sm">
              <span>{k.desc}</span>
              <kbd class="shrink-0 rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{k.combo}</kbd>
            </p>
          {/each}
        </div>
      {/each}
      <button class="mt-4 w-full rounded-md border border-input px-2.5 py-1.5 text-xs hover:bg-muted" onclick={() => (open = false)}>Close</button>
    </div>
  </div>
{/if}
