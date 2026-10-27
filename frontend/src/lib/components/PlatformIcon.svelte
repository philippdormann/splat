<script lang="ts">
  import { FileCode2, Braces, Globe, Coffee, Boxes } from "lucide-svelte";

  export let platform = "";
  export let size = 24;

  function iconFor(id: string) {
    if (id.startsWith("python")) return FileCode2;
    if (id.startsWith("java")) return Coffee;
    if (id === "javascript-browser") return Globe;
    if (id === "bun" || id === "nodejs" || id === "nextjs") return Braces;
    return Boxes;
  }
</script>

<svelte:component this={iconFor(platform ?? "")} width={size} height={size} />
