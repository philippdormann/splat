<script lang="ts">
  import { cn } from "$lib/utils";
  export let variant: "default" | "secondary" | "outline" | "destructive" = "secondary";
  const variants: Record<string, string> = {
    default: "bg-primary text-primary-foreground",
    secondary: "bg-secondary text-secondary-foreground",
    outline: "border border-border",
    destructive: "bg-destructive text-white",
  };
</script>

<span class={cn("inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-semibold", variants[variant], $$props.class)}>
  <slot />
</span>
