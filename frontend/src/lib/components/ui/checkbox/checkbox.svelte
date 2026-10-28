<script lang="ts">
  import { Checkbox } from "bits-ui";
  import { cn } from "$lib/utils";
  import { Check, Minus } from "lucide-svelte";

  let {
    checked = $bindable(false),
    indeterminate = false,
    class: className = "",
    ...rest
  }: {
    checked?: boolean;
    indeterminate?: boolean;
    class?: string;
  } & Record<string, unknown> = $props();
</script>

<Checkbox.Root
  bind:checked
  data-slot="checkbox"
  class={cn(
    "border-input dark:bg-input/30 focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive data-[state=checked]:bg-primary data-[state=checked]:text-primary-foreground dark:data-[state=checked]:bg-primary data-[state=checked]:border-primary focus-visible:ring-[3px] aria-invalid:ring-[3px] peer size-4 shrink-0 rounded-[4px] border shadow-xs transition-shadow outline-none disabled:cursor-not-allowed disabled:opacity-50",
    className,
  )}
  {...rest}
>
  {#snippet children({ checked: isChecked, indeterminate: isIndeterminate })}
    <span data-slot="checkbox-indicator" class="flex items-center justify-center text-current transition-none">
      {#if isIndeterminate || indeterminate}
        <Minus class="size-3.5" />
      {:else if isChecked}
        <Check class="size-3.5" />
      {/if}
    </span>
  {/snippet}
</Checkbox.Root>
