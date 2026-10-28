<script lang="ts">
  import { AlertDialog } from "bits-ui";

  let {
    open = $bindable(false),
    ...rest
  }: { open?: boolean } & Record<string, unknown> = $props();
</script>

<AlertDialog.Root bind:open {...rest} />
