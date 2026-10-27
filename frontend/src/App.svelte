<script lang="ts">
  import { onMount } from "svelte";
  import {
    Bug,
    LayoutDashboard,
    TriangleAlert,
    Folder,
    FolderPlus,
    Settings,
    LogOut,
    Menu,
    ChevronDown,
    Plus,
  } from "lucide-svelte";
  import { api } from "$lib/api";
  import { Toaster } from "svelte-sonner";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";
  import Login from "$lib/pages/Login.svelte";
  import Dashboard from "$lib/pages/Dashboard.svelte";
  import Issues from "$lib/pages/Issues.svelte";
  import IssueDetail from "$lib/pages/IssueDetail.svelte";
  import Projects from "$lib/pages/Projects.svelte";
  import ProjectDetail from "$lib/pages/ProjectDetail.svelte";
  import ProjectNew from "$lib/pages/ProjectNew.svelte";
  import Releases from "$lib/pages/Releases.svelte";
  import SettingsPage from "$lib/pages/Settings.svelte";
  import CommandPalette from "$lib/components/CommandPalette.svelte";

  let authed: boolean | null = null;
  let isAdmin = false;
  let path = window.location.pathname;
  let sidebarOpen = false;
  let paletteOpen = false;
  let navProjects: { id: number; name: string }[] = [];

  async function loadNavProjects() {
    try {
      navProjects = await api.projects();
    } catch {
      navProjects = [];
    }
  }

  // Full auto theme: follow the OS color scheme, live. No manual toggle,
  // no stored preference.
  function applyTheme(e?: MediaQueryListEvent) {
    const dark = e ? e.matches : window.matchMedia("(prefers-color-scheme: dark)").matches;
    document.documentElement.classList.toggle("dark", dark);
  }

  onMount(async () => {
    applyTheme();
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = (e: MediaQueryListEvent) => applyTheme(e);
    mq.addEventListener("change", onChange);
    window.addEventListener("popstate", () => {
      path = window.location.pathname;
    });
    try {
      const me = await api.me();
      authed = me.authenticated;
      isAdmin = me.role === "admin";
      if (authed) loadNavProjects();
    } catch {
      authed = false;
    }
  });

  export function navigate(to: string) {
    window.history.pushState({}, "", to);
    path = to;
    sidebarOpen = false;
    window.scrollTo(0, 0);
  }

  async function logout() {
    await api.logout();
    authed = false;
    isAdmin = false;
    navigate("/login");
  }

  function onGlobalKeydown(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
      e.preventDefault();
      if (authed) paletteOpen = !paletteOpen;
      return;
    }
    if (e.key === "Escape" && sidebarOpen) {
      sidebarOpen = false;
      return;
    }
    // Single-key shortcuts (ignored while typing).
    const tag = (e.target as HTMLElement | null)?.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || !authed || paletteOpen) return;
    if (e.shiftKey && !e.metaKey && !e.ctrlKey && !e.altKey) {
      if (e.key === "I") navigate("/issues");
      else if (e.key === "P") navigate("/projects");
      else if (e.key === "S") navigate("/settings");
    }
  }

  $: if (authed === false && path !== "/login") {
    navigate("/login");
  }

  function navClass(active: boolean) {
    return (
      "flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors " +
      (active
        ? "bg-primary text-primary-foreground"
        : "text-muted-foreground hover:bg-muted hover:text-foreground")
    );
  }
</script>

<svelte:window onkeydown={onGlobalKeydown} />

{#if authed === null}
  <p class="p-6 text-sm text-muted-foreground">Loading…</p>
{:else if !authed}
  <Login onLoggedIn={async () => { authed = true; try { isAdmin = (await api.me()).role === "admin"; } catch { isAdmin = false; } loadNavProjects(); navigate("/"); }} />
{:else}
  <CommandPalette bind:open={paletteOpen} {navigate} {logout} />
  <div class="flex min-h-screen">
    {#if sidebarOpen}
      <button aria-label="Close menu" class="fixed inset-0 z-10 bg-black/50 md:hidden" on:click={() => (sidebarOpen = false)} />
    {/if}
    <aside class={`fixed inset-y-0 left-0 z-20 w-60 border-r border-border bg-muted/50 p-4 transition-transform md:static md:translate-x-0 ${sidebarOpen ? "translate-x-0" : "-translate-x-full"}`}>
      <div class="mb-4 flex items-center gap-2 font-semibold"><Bug class="h-5 w-5" /> Errgotrack</div>
      <nav class="space-y-1">
        <a href="/" class={navClass(path === "/")} on:click|preventDefault={() => navigate("/")}><LayoutDashboard class="h-4 w-4" /> Dashboard</a>
        <a href="/issues" class={navClass(path.startsWith("/issues"))} on:click|preventDefault={() => navigate("/issues")}><TriangleAlert class="h-4 w-4" /> Issues</a>
        <DropdownMenu.Root>
          <DropdownMenu.Trigger
            class={path.startsWith("/projects") ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground"}
            onclick={loadNavProjects}
          >
            <Folder class="h-4 w-4" />
            <span class="flex-1 text-left">Projects</span>
            <ChevronDown class="h-3.5 w-3.5 opacity-60" />
          </DropdownMenu.Trigger>
          <DropdownMenu.Content side="right" align="start" class="w-56">
            <DropdownMenu.Label>Projects</DropdownMenu.Label>
            <DropdownMenu.Item onSelect={() => navigate("/projects")}>
              <Folder class="h-4 w-4" /><span>All projects</span>
            </DropdownMenu.Item>
            {#if navProjects.length}
              <DropdownMenu.Separator />
              {#each navProjects as p (p.id)}
                <DropdownMenu.Item onSelect={() => navigate(`/projects/${p.id}`)}>
                  <span class="truncate">{p.name}</span>
                </DropdownMenu.Item>
              {/each}
            {/if}
            {#if isAdmin}
              <DropdownMenu.Separator />
              <DropdownMenu.Item onSelect={() => navigate("/projects/new")}>
                <Plus class="h-4 w-4" /><span>New project</span>
              </DropdownMenu.Item>
            {/if}
          </DropdownMenu.Content>
        </DropdownMenu.Root>
        {#if isAdmin}
          <a href="/settings" class={navClass(path === "/settings")} on:click|preventDefault={() => navigate("/settings")}><Settings class="h-4 w-4" /> Settings</a>
        {/if}
      </nav>
      <div class="mt-8">
        <button class={navClass(false)} on:click={logout}><LogOut class="h-4 w-4" /> Log out</button>
      </div>
    </aside>

    <div class="flex min-w-0 flex-1 flex-col">
      <header class="flex items-center justify-between border-b border-border p-3">
        <button class="rounded-md p-2 hover:bg-muted md:hidden" on:click={() => (sidebarOpen = !sidebarOpen)} aria-label="Menu"><Menu class="h-5 w-5" /></button>
        <button class="hidden items-center gap-2 rounded-md border border-input px-2.5 py-1.5 text-xs text-muted-foreground hover:bg-muted md:flex" on:click={() => (paletteOpen = true)}>
          <span>Search or command…</span>
          <kbd class="rounded bg-muted px-1 font-mono">⌘K</kbd>
        </button>
      </header>
      <main class="mx-auto w-full max-w-6xl flex-1 p-4 md:p-6">
        {#if path === "/"}
          <Dashboard {navigate} />
        {:else if path === "/issues"}
          <Issues {navigate} />
        {:else if path.startsWith("/issues/")}
          <IssueDetail id={path.slice("/issues/".length)} {navigate} />
        {:else if path === "/projects"}
          <Projects {navigate} />
        {:else if path === "/projects/new"}
          <ProjectNew {navigate} />
        {:else if path.startsWith("/projects/")}
          {#if path.endsWith("/releases")}
            <Releases id={path.slice("/projects/".length, -"/releases".length)} {navigate} />
          {:else}
            <ProjectDetail id={path.slice("/projects/".length)} {navigate} />
          {/if}
        {:else if path === "/settings"}
          <SettingsPage />
        {:else if path === "/login"}
          <Dashboard {navigate} />
        {:else}
          <p class="text-sm">Not found. <a href="/" class="underline" on:click|preventDefault={() => navigate("/")}>Home</a></p>
        {/if}
      </main>
    </div>
  </div>
{/if}
<Toaster richColors closeButton position="bottom-right" />
