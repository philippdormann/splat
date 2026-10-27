<script lang="ts">
  import { cn } from "$lib/utils";
  export let value: string | number = "";
  export let type = "text";
  export let placeholder = "";
  export let name = "";
  export let id = "";
  export let required = false;
  export let disabled = false;
</script>

<input
  {type}
  {placeholder}
  {name}
  {id}
  {required}
  {disabled}
  bind:value
  on:input
  on:change
  class={cn("flex h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50", $$props.class)}
/>
