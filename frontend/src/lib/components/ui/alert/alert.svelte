<script lang="ts">
  import { cn } from "$lib/utils";
  import type { Snippet } from "svelte";

  const variants: Record<string, string> = {
    default: "bg-card text-card-foreground",
    destructive:
      "text-destructive bg-card [&>svg]:text-current *:data-[slot=alert-description]:text-destructive/90",
  };

  let {
    class: className = "",
    variant = "default",
    children,
    ...rest
  }: { class?: string; variant?: "default" | "destructive"; children?: Snippet } & Record<string, unknown> =
    $props();
</script>

<div
  data-slot="alert"
  role="alert"
  class={cn(
    "relative grid w-full grid-cols-[0_1fr] items-start gap-y-0.5 rounded-lg border px-4 py-3 text-sm has-[>svg]:grid-cols-[calc(var(--spacing)*4)_1fr] has-[>svg]:gap-x-3 [&>svg]:size-4 [&>svg]:translate-y-0.5 [&>svg]:text-current",
    variants[variant],
    className,
  )}
  {...rest}
>
  {@render children?.()}
</div>
