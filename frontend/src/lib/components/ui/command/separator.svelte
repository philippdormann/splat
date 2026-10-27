<script lang="ts">
  import { Command } from "bits-ui";
  import { cn } from "$lib/utils";

  let { class: className = "", ...rest }: { class?: string } & Record<string, unknown> = $props();
</script>

<Command.Separator class={cn("-mx-1 h-px bg-border", className)} {...rest} />
