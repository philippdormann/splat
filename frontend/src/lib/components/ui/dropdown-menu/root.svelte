<script lang="ts">
  import { DropdownMenu } from "bits-ui";
  import type { Snippet } from "svelte";

  let {
    children,
    ...rest
  }: { children?: Snippet } & Record<string, unknown> = $props();
</script>

<DropdownMenu.Root {...rest}>
  {@render children?.()}
</DropdownMenu.Root>
