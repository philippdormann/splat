<script lang="ts">
  import { cn } from "$lib/utils";
  export let cls = "";
</script>

<div class={cn("rounded-lg border border-border bg-card text-card-foreground shadow-sm", cls)}>
  <slot />
</div>
