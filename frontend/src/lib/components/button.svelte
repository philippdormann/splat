<script lang="ts">
  import { cn } from "$lib/utils";
  export let variant: "default" | "secondary" | "outline" | "ghost" | "destructive" = "default";
  export let size: "sm" | "default" | "lg" | "icon" = "default";
  export let href: string | undefined = undefined;
  export let type: "button" | "submit" = "button";
  export let disabled = false;

  const variants: Record<string, string> = {
    default: "bg-primary text-primary-foreground hover:bg-primary/90",
    secondary: "bg-secondary text-secondary-foreground hover:bg-secondary/80",
    outline: "border border-input bg-background hover:bg-muted",
    ghost: "hover:bg-muted",
    destructive: "bg-destructive text-white hover:bg-destructive/90",
  };
  const sizes: Record<string, string> = {
    sm: "h-8 px-3 text-xs",
    default: "h-9 px-4 text-sm",
    lg: "h-10 px-6 text-sm",
    icon: "h-9 w-9",
  };
</script>

{#if href}
  <a {href} class={cn("inline-flex cursor-pointer items-center justify-center gap-2 rounded-md font-medium transition-colors disabled:opacity-50", variants[variant], sizes[size], $$props.class)} on:click>
    <slot />
  </a>
{:else}
  <button {type} {disabled} class={cn("inline-flex cursor-pointer items-center justify-center gap-2 rounded-md font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50", variants[variant], sizes[size], $$props.class)} on:click>
    <slot />
  </button>
{/if}
