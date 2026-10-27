<script lang="ts">
  import { cn } from "$lib/utils";
</script>

<div class={cn("overflow-x-auto rounded-lg border border-border", $$props.class)}>
  <table class="w-full text-sm">
    <slot />
  </table>
</div>
