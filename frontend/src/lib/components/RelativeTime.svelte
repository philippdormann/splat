<script lang="ts">
  import { now, formatRelative, formatLocal, formatUTC } from "$lib/time";

  export let iso: string;
  export let cls = "";
</script>

<span class={cls} title={`${formatLocal(iso)} (local) · ${formatUTC(iso)}`}>{formatRelative(iso, $now)}</span>
