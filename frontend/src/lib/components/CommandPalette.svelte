<script lang="ts">
  import {
    LayoutDashboard,
    TriangleAlert,
    Folder,
    FolderPlus,
    Settings,
    LogOut,
  } from "lucide-svelte";
  import * as Command from "$lib/components/ui/command/index.js";
  import { api } from "$lib/api";

  let {
    open = $bindable(false),
    navigate,
    logout,
  }: {
    open?: boolean;
    navigate: (to: string) => void;
    logout: () => void;
  } = $props();

  let query = $state("");
  let projects = $state<{ id: number; name: string }[]>([]);
  let issueResults = $state<{ id: number; title: string; status: string; projectName: string }[]>([]);
  let panel = $state<HTMLElement | null>(null);
  let searchTimer: ReturnType<typeof setTimeout>;
  let searchSeq = 0;

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

  // Debounced issue search so on-call can jump straight to an error.
  $effect(() => {
    const term = query.trim();
    clearTimeout(searchTimer);
    if (!open || term.length < 2) {
      issueResults = [];
      return;
    }
    searchTimer = setTimeout(async () => {
      const mine = ++searchSeq;
      try {
        const params = new URLSearchParams({ q: term, page: "1" });
        const res = await api.issues(params);
        if (mine === searchSeq) issueResults = res.issues.slice(0, 7);
      } catch {
        if (mine === searchSeq) issueResults = [];
      }
    }, 250);
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
            <Command.Item value="logout" keywords={["sign out", "exit"]} onSelect={doLogout}>
              <LogOut /><span>Log out</span>
            </Command.Item>
          </Command.Group>
          {#if issueResults.length}
            <Command.Separator />
            <Command.Group heading="Issues">
              {#each issueResults as r (r.id)}
                <Command.Item
                  value={`issue ${r.title}`}
                  keywords={[r.title, String(r.id)]}
                  onSelect={() => go(`/issues/${r.id}`)}
                >
                  <TriangleAlert />
                  <span class="min-w-0 flex-1 truncate">{r.title}</span>
                  <span class="text-muted-foreground shrink-0 text-xs">{r.projectName}</span>
                </Command.Item>
              {/each}
            </Command.Group>
          {/if}
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
