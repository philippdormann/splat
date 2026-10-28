<script lang="ts">
  import * as AlertDialog from "$lib/components/ui/alert-dialog/index.js";
  import { LoaderCircle } from "lucide-svelte";

  // Confirmation for deleting one project or a ticked batch.
  // targetName distinguishes the two: a named target is always exactly one.
  export let open = false;
  export let count = 0;
  export let targetName: string | undefined = undefined;
  export let busy = false;
  export let onConfirm: () => void = () => {};
  export let onCancel: () => void = () => {};

  $: subject = targetName ? 1 : count;
</script>

<AlertDialog.Root bind:open onOpenChange={(o: boolean) => { if (!o) onCancel(); }}>
  <AlertDialog.Content size="sm">
    <AlertDialog.Header>
      <AlertDialog.Title>
        {#if targetName}Delete “{targetName}”?{:else}Delete {count} project{count === 1 ? "" : "s"}?{/if}
      </AlertDialog.Title>
      <AlertDialog.Description>
        {#if targetName}This project and all its issues, events, logs and traces will be gone forever.
        {:else}These {subject} project{subject === 1 ? "" : "s"} and everything in them will be gone forever.{/if}
        This cannot be undone.
      </AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel disabled={busy}>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action
        disabled={busy}
        class="bg-destructive text-destructive-foreground hover:bg-destructive/90"
        onclick={onConfirm}
      >
        {#if busy}<LoaderCircle class="mr-2 size-4 animate-spin" /> Deleting…{:else}Delete{/if}
      </AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
