<script lang="ts">
  import { Command } from "bits-ui";
  import { Search } from "lucide-svelte";
  import { cn } from "$lib/utils";

  let {
    class: className = "",
    value = $bindable(""),
    placeholder = "Type a command or search...",
    ...rest
  }: {
    class?: string;
    value?: string;
    placeholder?: string;
  } & Record<string, unknown> = $props();
</script>

<div class="flex items-center border-b border-border px-3" data-command-input-wrapper>
  <Search class="mr-2 h-4 w-4 shrink-0 text-muted-foreground" />
  <Command.Input
    class={cn(
      "flex h-11 w-full rounded-md bg-transparent py-3 text-sm outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50",
      className,
    )}
    bind:value
    {placeholder}
    {...rest}
  />
</div>
