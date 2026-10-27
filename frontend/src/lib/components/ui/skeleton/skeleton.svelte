<script lang="ts">
  import { cn } from "$lib/utils";

  let { class: className = "", ...rest }: { class?: string } & Record<string, unknown> = $props();
</script>

<div data-slot="skeleton" class={cn("bg-muted animate-pulse rounded-md", className)} {...rest}></div>
