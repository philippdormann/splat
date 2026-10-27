<script lang="ts">
  import { formatLocal, formatUTC, formatRelative, now } from "$lib/time";

  export let iso: string;
  export let cls = "";
</script>

<span class={cls} title={`${formatUTC(iso)} · ${formatRelative(iso, $now)}`}>{formatLocal(iso)} <span class="text-muted-foreground">local</span></span>
