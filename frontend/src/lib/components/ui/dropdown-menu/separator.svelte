<script lang="ts">
  import { DropdownMenu } from "bits-ui";
  import { cn } from "$lib/utils";

  let { class: className = "", ...rest }: { class?: string } & Record<string, unknown> = $props();
</script>

<DropdownMenu.Separator class={cn("-mx-1 my-1 h-px bg-border", className)} {...rest} />
