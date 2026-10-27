<script lang="ts">
  import {
    LayoutDashboard,
    TriangleAlert,
    Folder,
    FolderPlus,
    Settings,
    Sun,
    Moon,
    LogOut,
  } from "lucide-svelte";
  import * as Command from "$lib/components/ui/command/index.js";
  import { api } from "$lib/api";

  let {
    open = $bindable(false),
    navigate,
    toggleTheme,
    isDark,
    logout,
  }: {
    open?: boolean;
    navigate: (to: string) => void;
    toggleTheme: () => void;
    isDark: boolean;
    logout: () => void;
  } = $props();

  let query = $state("");
  let projects = $state<{ id: number; name: string }[]>([]);
  let panel = $state<HTMLElement | null>(null);

  async function loadProjects() {
    try {
      projects = await api.projects();
    } catch {
      projects = [];
    }
  }

  function go(to: string) {
    open = false;
    navigate(to);
  }

  function doTheme() {
    open = false;
    toggleTheme();
  }

  function doLogout() {
    open = false;
    logout();
  }

  $effect(() => {
    if (open) {
      query = "";
      loadProjects();
      // Focus the filter input once the panel is in the DOM.
      queueMicrotask(() => {
        panel?.querySelector("input")?.focus();
      });
    }
  });

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") open = false;
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div
    class="fixed inset-0 z-50 flex items-start justify-center bg-black/50 p-4 pt-[15vh]"
    onclick={(e) => {
      if (e.target === e.currentTarget) open = false;
    }}
  >
    <div
      bind:this={panel}
      class="w-full max-w-lg overflow-hidden rounded-lg border border-border bg-card shadow-lg"
      role="dialog"
      aria-label="Command palette"
    >
      <Command.Root loop>
        <Command.Input bind:value={query} placeholder="Type a command or search…" />
        <Command.List>
          <Command.Empty>No results found.</Command.Empty>
          <Command.Group heading="Navigation">
            <Command.Item value="dashboard" keywords={["home", "overview"]} onSelect={() => go("/")}>
              <LayoutDashboard /><span>Dashboard</span>
            </Command.Item>
            <Command.Item value="issues" keywords={["errors", "bugs", "list"]} onSelect={() => go("/issues")}>
              <TriangleAlert /><span>Issues</span>
              <Command.Shortcut>⇧I</Command.Shortcut>
            </Command.Item>
            <Command.Item value="projects" keywords={["apps", "dsn", "list"]} onSelect={() => go("/projects")}>
              <Folder /><span>Projects</span>
              <Command.Shortcut>⇧P</Command.Shortcut>
            </Command.Item>
            <Command.Item value="new project" keywords={["create", "add", "app"]} onSelect={() => go("/projects/new")}>
              <FolderPlus /><span>New project</span>
            </Command.Item>
            <Command.Item value="settings" keywords={["config", "notifications", "alerts"]} onSelect={() => go("/settings")}>
              <Settings /><span>Settings</span>
              <Command.Shortcut>⇧S</Command.Shortcut>
            </Command.Item>
          </Command.Group>
          <Command.Separator />
          <Command.Group heading="Actions">
            <Command.Item value="toggle theme" keywords={["dark", "light", "mode"]} onSelect={doTheme}>
              {#if isDark}<Sun /><span>Light mode</span>{:else}<Moon /><span>Dark mode</span>{/if}
            </Command.Item>
            <Command.Item value="logout" keywords={["sign out", "exit"]} onSelect={doLogout}>
              <LogOut /><span>Log out</span>
            </Command.Item>
          </Command.Group>
          {#if projects.length}
            <Command.Separator />
            <Command.Group heading="Projects">
              {#each projects as p (p.id)}
                <Command.Item
                  value={`project ${p.name}`}
                  keywords={[p.name, String(p.id)]}
                  onSelect={() => go(`/projects/${p.id}`)}
                >
                  <Folder /><span>{p.name}</span>
                </Command.Item>
              {/each}
            </Command.Group>
          {/if}
        </Command.List>
      </Command.Root>
    </div>
  </div>
{/if}
