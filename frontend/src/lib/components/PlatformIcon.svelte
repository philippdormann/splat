<script lang="ts">
  import {
    FileCode2,
    Braces,
    Globe,
    Coffee,
    Boxes,
    Smartphone,
    Apple,
    Gamepad2,
  } from "lucide-svelte";

  export let platform = "";
  export let language = "";
  export let size = 24;

  // Language-based mapping (mirrors rustrak's language groups): frameworks
  // under one language share an icon so ~110 platforms stay recognizable
  // without a 110-entry switch.
  function iconFor(id: string, lang: string) {
    const l = (lang || "").toLowerCase();
    const pid = (id || "").toLowerCase();
    if (l === "javascript" || l === "node" || pid.startsWith("javascript") || pid.startsWith("node")) {
      if (pid.includes("react") || pid.includes("next") || pid.includes("vue") || pid.includes("angular") || pid.includes("svelte") || pid.includes("nuxt") || pid.includes("remix")) return Globe;
      return Braces;
    }
    if (l === "python" || pid.startsWith("python")) return FileCode2;
    if (l === "java" || l === "kotlin" || pid.startsWith("java")) return Coffee;
    if (l === "go" || pid.startsWith("go-") || pid === "go") return Braces;
    if (l === "ruby" || pid.startsWith("ruby")) return FileCode2;
    if (l === "php" || pid.startsWith("php")) return FileCode2;
    if (l === "rust" || l === "csharp" || l === "dotnet" || l === "c" || l === "native") return Boxes;
    if (l === "apple" || l === "cocoa" || l === "objc" || pid.startsWith("apple")) return Apple;
    if (l === "android" || l === "mobile" || l === "capacitor" || l === "cordova" || l === "ionic" || pid.includes("android") || pid.includes("ios")) return Smartphone;
    if (l === "bun" || l === "deno") return Braces;
    if (l === "dart" || l === "flutter" || l === "react-native") return Smartphone;
    if (l === "elixir" || l === "haskell" || l === "groovy" || l === "perl" || l === "powershell" || l === "cfml") return FileCode2;
    if (pid.includes("switch") || pid.includes("playstation") || pid.includes("xbox") || pid.includes("unity") || pid.includes("unreal") || pid.includes("godot")) return Gamepad2;
    return Boxes;
  }
</script>

<svelte:component this={iconFor(platform ?? "", language ?? "")} width={size} height={size} />
