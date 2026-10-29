<script lang="ts">
  import { Dialog } from "bits-ui";

  let {
    open = $bindable(false),
    ...rest
  }: { open?: boolean } & Record<string, unknown> = $props();
</script>

<Dialog.Root bind:open {...rest} />
