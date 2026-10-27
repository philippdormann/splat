<script lang="ts">
  import { onMount } from "svelte";
  import { api, ApiError, type Platform, type ProjectListItem } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import PlatformGrid from "$lib/components/PlatformGrid.svelte";
  import PlatformIcon from "$lib/components/PlatformIcon.svelte";
  import { ArrowLeft, Pencil, RotateCcw, LoaderCircle } from "lucide-svelte";
  import { cn } from "$lib/utils";

  export let navigate: (path: string) => void;

  let platform = "";
  let name = "";
  let slug = "";
  let slugManual = false;
  let defaultEnvironment = "production";
  let retentionDays = 0;
  let platforms: Platform[] = [];
  let existing: ProjectListItem[] = [];
  let fieldError = "";
  let error = "";
  let busy = false;

  onMount(async () => {
    try {
      [platforms, existing] = [await api.platforms(), await api.projects()];
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load.";
    }
  });

  function slugify(s: string) {
    return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
  }

  function takenSlugs(): Set<string> {
    return new Set(existing.map((p) => p.slug));
  }

  function handlePlatformChange(id: string) {
    platform = id;
    fieldError = "";
    // Only auto-fill while the user has not taken over the name field.
    if (!nameManuallyEdited) {
      const label = platforms.find((p) => p.id === id)?.label ?? id;
      name = label;
      if (!slugManual) slug = slugify(label);
    }
  }

  let nameManuallyEdited = false;

  function onNameInput() {
    nameManuallyEdited = true;
    if (!slugManual) slug = slugify(name);
  }

  function toggleSlug() {
    slugManual = !slugManual;
    if (!slugManual) slug = slugify(name);
  }

  async function submit(e: Event) {
    e.preventDefault();
    busy = true;
    error = "";
    fieldError = "";
    try {
      if (!platform) {
        fieldError = "Pick a platform first.";
        return;
      }
      // Normalize a hand-typed slug the same way the auto slug is built.
      if (slugManual) slug = slugify(slug);
      const created = await api.createProject({ name, slug, defaultEnvironment, platform, retentionDays });
      navigate(`/projects/${created.project.id}`);
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "Could not create project.";
      // A taken slug belongs on its own field: hand a read-only slug over
      // so a server-rejected value is always fixable.
      if (/slug/i.test(msg)) {
        fieldError = msg;
        slugManual = true;
      } else {
        error = msg;
      }
    } finally {
      busy = false;
    }
  }

  $: selectedLabel = platforms.find((p) => p.id === platform)?.label ?? "";
  $: selectedLanguage = platforms.find((p) => p.id === platform)?.language ?? "";
  $: slugTaken = slug !== "" && takenSlugs().has(slug);

  // Picker categories mirror rustrak's PLATFORM_CATEGORIES (popular first,
  // default tab). `all` resolves from the server list so it never falls
  // behind. Search deliberately ignores the active tab.
  const categories: { id: string; label: string; platforms?: string[] }[] = [
    { id: "popular", label: "Popular", platforms: ["javascript-nextjs","javascript-react","react-native","node","php-laravel","python-fastapi","flutter","python-django","python","node-express","javascript","php","ruby-rails","apple-ios","node-nestjs","python-flask","javascript-vue","dotnet-aspnetcore","javascript-nuxt","dotnet-maui","javascript-angular","android","java-spring-boot","php-symfony","node-cloudflare-workers","electron","unity","javascript-remix"] },
    { id: "browser", label: "Browser", platforms: ["dart","flutter","javascript","javascript-angular","javascript-astro","javascript-ember","javascript-gatsby","javascript-nextjs","javascript-nuxt","javascript-react","javascript-react-router","javascript-remix","javascript-solid","javascript-solidstart","javascript-svelte","javascript-sveltekit","javascript-tanstackstart-react","javascript-vue","react-native","unity"] },
    { id: "server", label: "Server", platforms: ["bun","dart","deno","dotnet","dotnet-aspnet","dotnet-aspnetcore","elixir","go","go-echo","go-fasthttp","go-fiber","go-gin","go-http","go-iris","go-negroni","java","java-log4j2","java-logback","java-spring","java-spring-boot","kotlin","native","node","node-cloudflare-pages","node-cloudflare-workers","node-connect","node-express","node-fastify","node-hapi","node-hono","node-koa","node-nestjs","php","php-laravel","php-symfony","powershell","python","python-aiohttp","python-asgi","python-bottle","python-celery","python-chalice","python-django","python-falcon","python-fastapi","python-flask","python-litestar","python-pyramid","python-quart","python-rq","python-sanic","python-starlette","python-tornado","python-tryton","python-wsgi","ruby","ruby-rack","ruby-rails","rust"] },
    { id: "mobile", label: "Mobile", platforms: ["android","apple-ios","capacitor","cordova","dart","dotnet-maui","dotnet-xamarin","flutter","ionic","react-native","unity","unreal"] },
    { id: "desktop", label: "Desktop", platforms: ["apple-macos","dart","dotnet","dotnet-maui","dotnet-winforms","dotnet-wpf","electron","flutter","godot","java","kotlin","minidump","native","native-qt","unity","unreal"] },
    { id: "serverless", label: "Serverless", platforms: ["dotnet-awslambda","dotnet-gcpfunctions","node-awslambda","node-azurefunctions","node-cloudflare-pages","node-cloudflare-workers","node-gcpfunctions","python-awslambda","python-gcpfunctions","python-serverless"] },
    { id: "gaming", label: "Gaming", platforms: ["godot","native","nintendo-switch","playstation","unity","unreal","xbox"] },
    { id: "all", label: "All" },
  ];
  let category = "popular";
  let query = "";

  function visiblePlatforms(all: Platform[], q: string, cat: string): Platform[] {
    const needle = q.trim().toLowerCase();
    if (needle) {
      return all
        .filter((p) => p.id.toLowerCase().includes(needle) || p.label.toLowerCase().includes(needle) || p.language.toLowerCase().includes(needle))
        .sort((a, b) => a.label.localeCompare(b.label));
    }
    const categoryDef = categories.find((c) => c.id === cat);
    if (!categoryDef || !categoryDef.platforms) {
      return [...all].sort((a, b) => a.label.localeCompare(b.label));
    }
    const byId = new Map(all.map((p) => [p.id, p]));
    const resolved = (categoryDef.platforms ?? []).map((id) => byId.get(id)).filter((p): p is Platform => p !== undefined);
    return cat === "popular" ? resolved : resolved.sort((a, b) => a.label.localeCompare(b.label));
  }
  // Pass state as arguments: Svelte's legacy reactivity only re-runs on
  // variables named in the statement, not on reads inside the function.
  $: filtered = visiblePlatforms(platforms, query, category);
</script>

<div class="mx-auto w-full max-w-6xl">
  <button class="inline-flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground" on:click={() => navigate("/projects")}>
    <ArrowLeft class="h-4 w-4" /> Back to projects
  </button>

  <div class="mb-6 mt-4">
    <h1 class="text-xl font-extrabold tracking-tight md:text-2xl">New project</h1>
    <p class="mt-1 text-muted-foreground">Pick a platform, name it, get a DSN.</p>
  </div>

  <form on:submit={submit} class="grid gap-6 pb-24 lg:grid-cols-[minmax(0,1fr)_20rem] lg:items-start lg:pb-0">
    <div class="min-w-0">
      <p class="mb-2 text-xs font-bold uppercase tracking-widest text-muted-foreground">Platform</p>
      <div class="mb-3 flex flex-wrap gap-1">
        {#each categories as c}
          <Button size="sm" variant={category === c.id && !query.trim() ? "default" : "outline"} on:click={() => { category = c.id; }}>{c.label}</Button>
        {/each}
      </div>
      <div class="mb-3"><Input bind:value={query} placeholder="Search platforms… (e.g. django, nextjs, go)" autocomplete="off" /></div>
      {#if !platforms.length && !error}
        <div class="grid grid-cols-2 gap-2 sm:grid-cols-3" aria-label="Loading platforms">
          <div class="h-24 animate-pulse rounded-lg bg-muted"></div>
          <div class="h-24 animate-pulse rounded-lg bg-muted"></div>
          <div class="h-24 animate-pulse rounded-lg bg-muted"></div>
          <div class="h-24 animate-pulse rounded-lg bg-muted"></div>
          <div class="h-24 animate-pulse rounded-lg bg-muted"></div>
          <div class="h-24 animate-pulse rounded-lg bg-muted"></div>
        </div>
      {:else if !filtered.length}
        <p class="rounded-md border border-border p-6 text-center text-sm text-muted-foreground">No platforms match “{query.trim()}”.</p>
      {:else}
        <PlatformGrid platforms={filtered} value={platform} disabled={busy} onSelect={handlePlatformChange} />
      {/if}
      {#if fieldError && !platform}<p class="mt-2 text-xs text-destructive">{fieldError}</p>{/if}
    </div>

    <aside class="rounded-xl border bg-card p-5 lg:sticky lg:top-6">
      <div class="flex items-center gap-3 border-b pb-4">
        {#if platform}
          <PlatformIcon {platform} language={selectedLanguage} size={32} />
          <div class="min-w-0">
            <p class="truncate text-sm font-bold">{selectedLabel}</p>
            <p class="truncate font-mono text-xs text-muted-foreground">{platform}</p>
          </div>
        {:else}
          <div class="size-8 shrink-0 rounded-md border border-dashed" />
          <p class="text-sm text-muted-foreground">No platform selected</p>
        {/if}
      </div>

      <div class="space-y-4 pt-4">
        <div class="space-y-2">
          <label class="text-xs font-bold uppercase tracking-widest text-muted-foreground" for="proj-name">Name</label>
          <Input id="proj-name" bind:value={name} on:input={onNameInput} required placeholder="My app" autocomplete="off" />
        </div>
        <div class="space-y-2">
          <label class="text-xs font-bold uppercase tracking-widest text-muted-foreground" for="proj-slug">Slug</label>
          <div class="flex items-center gap-2">
            <Input
              id="proj-slug"
              bind:value={slug}
              required
              placeholder="my-app"
              autocomplete="off"
              disabled={!slugManual}
              class={cn("font-mono", !slugManual && "bg-muted text-muted-foreground")}
            />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={slugManual ? "Reset slug to follow the name" : "Edit slug manually"}
              title={slugManual ? "Reset slug to follow the name" : "Edit slug manually"}
              on:click={toggleSlug}
              class="shrink-0"
            >
              {#if slugManual}<RotateCcw class="h-4 w-4" />{:else}<Pencil class="h-4 w-4" />{/if}
            </Button>
          </div>
          {#if fieldError && platform}
            <p class="text-xs text-destructive">{fieldError}</p>
          {:else if slugTaken}
            <p class="text-xs text-amber-600 dark:text-amber-400">That slug is already taken.</p>
          {:else}
            <p class="text-xs text-muted-foreground">{slugManual ? "Manual slug — it won't follow the name." : "Follows the name until you edit it."}</p>
          {/if}
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div class="space-y-2"><label class="text-xs font-bold uppercase tracking-widest text-muted-foreground" for="proj-env">Environment</label><Input id="proj-env" bind:value={defaultEnvironment} placeholder="production" /></div>
          <div class="space-y-2"><label class="text-xs font-bold uppercase tracking-widest text-muted-foreground" for="proj-ret">Retention (days, 0 = forever)</label><Input id="proj-ret" type="number" min="0" bind:value={retentionDays} /></div>
        </div>
      </div>

      {#if error}<p class="mt-4 text-sm text-destructive">{error}</p>{/if}

      <Button type="submit" disabled={busy} class="mt-5 hidden w-full lg:flex">
        {#if busy}<LoaderCircle class="mr-2 h-4 w-4 animate-spin" /> Creating…{:else}Create project{/if}
      </Button>
      <p class="mt-3 hidden text-xs text-muted-foreground lg:block">Next: copy the DSN from the project page.</p>
    </aside>

    <div class="fixed inset-x-0 bottom-0 z-40 border-t bg-background/95 px-4 py-3 backdrop-blur lg:hidden">
      <div class="mx-auto flex max-w-6xl items-center gap-3">
        {#if platform}
          <PlatformIcon {platform} language={selectedLanguage} size={24} />
          <span class="min-w-0 flex-1 truncate text-sm font-medium">{selectedLabel}</span>
        {:else}
          <span class="min-w-0 flex-1 truncate text-sm text-muted-foreground">Pick a platform</span>
        {/if}
        <Button type="submit" disabled={busy} size="sm" class="shrink-0">
          {#if busy}<LoaderCircle class="mr-2 h-4 w-4 animate-spin" /> Creating…{:else}Create{/if}
        </Button>
      </div>
    </div>
  </form>
</div>
