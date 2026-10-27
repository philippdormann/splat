<script lang="ts">
  import { Separator } from "bits-ui";
  import { cn } from "$lib/utils";

  let {
    class: className = "",
    orientation = "horizontal",
    ...rest
  }: { class?: string; orientation?: "horizontal" | "vertical" } & Record<string, unknown> = $props();
</script>

<Separator.Root
  data-slot="separator"
  {orientation}
  class={cn(
    "bg-border shrink-0 data-[orientation=horizontal]:h-px data-[orientation=horizontal]:w-full data-[orientation=vertical]:h-full data-[orientation=vertical]:w-px",
    className,
  )}
  {...rest}
/>
