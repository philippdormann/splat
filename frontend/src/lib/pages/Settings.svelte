<script lang="ts">
  import { onMount } from "svelte";
  import { api, type SettingsData, type StorageUsage, type AlertRule, type AlertRuleInput, type APIKey, type GenericWebhook, type User, type Team } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import { toast } from "svelte-sonner";
  import ConfirmDialog from "$lib/components/ConfirmDialog.svelte";
  import Dialog from "$lib/components/Dialog.svelte";
  import { Bell, KeyRound, Database, SlidersHorizontal, ScrollText, Users } from "lucide-svelte";

  let tab = "general";
  const tabs = [
    { id: "general", label: "General", icon: SlidersHorizontal },
    { id: "notifications", label: "Notifications", icon: Bell },
    { id: "rules", label: "Alert rules", icon: ScrollText },
    { id: "keys", label: "API keys", icon: KeyRound },
    { id: "users", label: "Users & Teams", icon: Users },
    { id: "storage", label: "Storage", icon: Database },
  ];

  let data: SettingsData | null = null;
  let error = "";
  let telegram = { botToken: "", chatId: "", enabled: false };
  let webhooks: Record<string, { url: string; enabled: boolean }> = {};
  let traceURL = "";
  let storage: StorageUsage | null = null;
  let storageBusy = "";
  let rules: AlertRule[] = [];
  let projects: { id: number; name: string }[] = [];
  let editingRule: (AlertRuleInput & { id?: number }) | null = null;
  let smtp = { host: "", port: 587, username: "", password: "", from: "", to: "", useTLS: true, enabled: false };
  let keys: APIKey[] = [];
  let newKey = { name: "", projectId: null as number | null, scopes: ["read"] as string[], expiresAt: "" };
  let freshSecret = "";
  let users: User[] = [];
  let teams: Team[] = [];
  let newUser = { username: "", password: "", role: "member" };
  let newTeam = { name: "", slug: "" };
  let showNewUser = false;
  let showNewTeam = false;
  let showNewKey = false;
  let meUsername = "";
  let resetPw: { id: number; username: string; password: string } | null = null;
  let teamAddUser: Record<number, number | null> = {};
  let teamAddProject: Record<number, number | null> = {};

  function usernameOf(id: number): string {
    return users.find((u) => u.id === id)?.username ?? `User ${id}`;
  }

  const channels = ["slack", "discord", "teams", "sheets"];
  const allChannels = ["telegram", "slack", "discord", "teams", "sheets", "email", "generic"];
  const allScopes = ["ingest", "read", "write"];

  let channelDialog: string | null = null;

  function closeChannelDialog() {
    channelDialog = null;
  }

  function telegramStatus(): string {
    if (!data) return "";
    if (data.telegram.enabled) return data.telegram.chatId ? `Chat ${data.telegram.chatId}` : "Enabled";
    return data.botTokenMasked ? "Configured, disabled" : "Not configured yet";
  }

  function webhookStatus(c: string): string {
    if (!data) return "";
    const w = data.webhooks[c];
    if (w?.enabled) return w.urlMasked || "Enabled";
    return w?.urlMasked ? "Configured, disabled" : "Not configured yet";
  }

  function smtpStatus(): string {
    if (!data) return "";
    if (data.smtp.enabled) return data.smtp.host || "Enabled";
    return data.smtp.host ? "Configured, disabled" : "Not configured yet";
  }

  let generic: GenericWebhook[] = [];
  let editingHook: { id?: number; name: string; url: string; secret: string; headersText: string; enabled: boolean } | null = null;

  function headersToText(h: Record<string, string>): string {
    return Object.entries(h ?? {}).map(([k, v]) => `${k}: ${v}`).join("\n");
  }

  function textToHeaders(t: string): Record<string, string> {
    const out: Record<string, string> = {};
    for (const line of t.split("\n")) {
      const i = line.indexOf(":");
      if (i < 0) continue;
      const k = line.slice(0, i).trim();
      const v = line.slice(i + 1).trim();
      if (k && v) out[k] = v;
    }
    return out;
  }

  function newHook() {
    editingHook = { name: "", url: "", secret: "", headersText: "", enabled: true };
  }

  function editHook(h: GenericWebhook) {
    editingHook = { id: h.id, name: h.name, url: h.url, secret: "", headersText: headersToText(h.headers), enabled: h.enabled };
  }

  async function saveHook(e: Event) {
    e.preventDefault();
    if (!editingHook) return;
    const input = { name: editingHook.name, url: editingHook.url, secret: editingHook.secret, headers: textToHeaders(editingHook.headersText), enabled: editingHook.enabled };
    if (editingHook.id) {
      await api.updateGenericWebhook(editingHook.id, input);
      toast.success("Generic webhook updated.");
    } else {
      await api.createGenericWebhook(input);
      toast.success("Generic webhook created.");
    }
    editingHook = null;
    generic = await api.genericWebhooks();
    data = await api.settings();
  }

  async function deleteHook(id: number) {
    confirmBox = {
      title: "Delete this generic webhook?",
      description: "Alert rules routing to the generic channel will skip the deleted endpoint.",
      label: "Delete",
      run: async () => {
        await api.deleteGenericWebhook(id);
        generic = await api.genericWebhooks();
        data = await api.settings();
        toast.success("Generic webhook deleted.");
      },
    };
  }

  function formatBytes(n: number): string {
    if (n < 1024) return `${n} B`;
    const units = ["KB", "MB", "GB", "TB"];
    let v = n / 1024;
    let u = 0;
    while (v >= 1024 && u < units.length - 1) {
      v /= 1024;
      u++;
    }
    return `${v.toFixed(1)} ${units[u]}`;
  }

  async function loadStorage() {
    try {
      storage = await api.storage();
    } catch (e) {
      toast.error(`Could not load storage usage: ${e instanceof Error ? e.message : e}`);
    }
  }

  async function runSweep() {
    storageBusy = "sweep";
    try {
      const r = await api.storageSweep();
      toast.success(`Retention sweep deleted ${r.deleted} event(s).`);
      await loadStorage();
    } catch (e) {
      toast.error(`Sweep failed: ${e instanceof Error ? e.message : e}`);
    } finally {
      storageBusy = "";
    }
  }

  async function runVacuum() {
    confirmBox = {
      title: "Vacuum the database?",
      description: "Vacuum rewrites the database file and may take a while on large databases.",
      label: "Vacuum",
      run: async () => {
        storageBusy = "vacuum";
        try {
          await api.storageVacuum();
          toast.success("Vacuum finished.");
          await loadStorage();
        } finally {
          storageBusy = "";
        }
      },
    };
  }

  onMount(async () => {
    try {
      data = await api.settings();
      telegram = { botToken: "", chatId: data.telegram.chatId, enabled: data.telegram.enabled };
      for (const c of channels) webhooks[c] = { url: "", enabled: data.webhooks[c]?.enabled ?? false };
      traceURL = data.traceURLTemplate;
      smtp = { host: data.smtp.host, port: data.smtp.port || 587, username: data.smtp.username, password: "", from: data.smtp.from, to: data.smtp.to, useTLS: data.smtp.useTLS, enabled: data.smtp.enabled };
      await loadStorage();
      rules = await api.rules();
      projects = await api.projects();
      keys = await api.apiKeys();
      generic = data.genericWebhooks ?? [];
      try {
        users = await api.users();
        teams = await api.teams();
        meUsername = (await api.me()).username ?? "";
      } catch (e) {
        toast.error(`Could not load users/teams: ${e instanceof Error ? e.message : e}`);
      }
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load settings.";
    }
  });

  async function saveTelegram(e: Event) {
    e.preventDefault();
    data = await api.saveTelegram(telegram);
    channelDialog = null;
    toast.success("Telegram settings saved.");
  }

  async function saveWebhook(e: Event, channel: string) {
    e.preventDefault();
    data = await api.saveWebhook(channel, webhooks[channel]);
    channelDialog = null;
    toast.success(`${channel} settings saved.`);
  }

  async function saveTracing(e: Event) {
    e.preventDefault();
    data = await api.saveTracing(traceURL);
    toast.success("Tracing settings saved.");
  }

  function projectName(pid: number | null): string {
    if (pid === null || pid === undefined) return "All projects";
    return projects.find((p) => p.id === pid)?.name ?? `Project ${pid}`;
  }

  function newRule() {
    editingRule = { name: "", projectId: null, environment: "", notifyNewIssue: true, notifyThreshold: 0, notifyReopened: true, cooldownSeconds: 300, channels: [] };
  }

  function editRule(r: AlertRule) {
    editingRule = { id: r.id, name: r.name, projectId: r.projectId, environment: r.environment, notifyNewIssue: r.notifyNewIssue, notifyThreshold: r.notifyThreshold, notifyReopened: r.notifyReopened, cooldownSeconds: r.cooldownSeconds, channels: [...(r.channels ?? [])] };
  }

  async function saveRule(e: Event) {
    e.preventDefault();
    if (!editingRule) return;
    const { id, ...input } = editingRule;
    if (id) {
      await api.updateRule(id, input);
      toast.success("Rule updated.");
    } else {
      await api.createRule(input);
      toast.success("Rule created.");
    }
    editingRule = null;
    rules = await api.rules();
  }

  async function deleteRule(id: number) {
    confirmBox = {
      title: "Delete this alert rule?",
      description: "Matching events will stop notifying on the rule's channels.",
      label: "Delete",
      run: async () => {
        await api.deleteRule(id);
        rules = await api.rules();
        toast.success("Rule deleted.");
      },
    };
  }

  function toggleRuleChannel(channel: string) {
    if (!editingRule) return;
    const list = editingRule.channels ?? [];
    editingRule.channels = list.includes(channel) ? list.filter((c) => c !== channel) : [...list, channel];
  }

  async function saveSMTP(e: Event) {
    e.preventDefault();
    data = await api.saveSMTP(smtp);
    channelDialog = null;
    toast.success("SMTP settings saved.");
  }

  function toggleScope(scope: string) {
    newKey.scopes = newKey.scopes.includes(scope)
      ? newKey.scopes.filter((s) => s !== scope)
      : [...newKey.scopes, scope];
  }

  async function createKey(e: Event) {
    e.preventDefault();
    let expires = newKey.expiresAt.trim();
    // Accept a plain date as shorthand for midnight UTC.
    if (/^\d{4}-\d{2}-\d{2}$/.test(expires)) expires += "T00:00:00Z";
    const k = await api.createAPIKey({
      name: newKey.name,
      projectId: newKey.projectId,
      scopes: newKey.scopes,
      expiresAt: expires || undefined,
    });
    freshSecret = k.secret ?? "";
    keys = await api.apiKeys();
    showNewKey = false;
    newKey = { name: "", projectId: null, scopes: ["read"], expiresAt: "" };
    toast.success("API key created — copy the secret now, it will never be shown again.");
  }

  async function rotateKey(id: number) {
    confirmBox = {
      title: "Rotate this key?",
      description: "The old secret stops working immediately. Copy the new secret once it appears.",
      label: "Rotate",
      run: async () => {
        const k = await api.rotateAPIKey(id);
        freshSecret = k.secret ?? "";
        keys = await api.apiKeys();
        toast.success("Key rotated — copy the new secret now.");
      },
    };
  }

  async function revokeKey(id: number) {
    confirmBox = {
      title: "Revoke this key?",
      description: "The secret stops working immediately. This cannot be undone — rotate instead if you just need a fresh secret.",
      label: "Revoke",
      run: async () => {
        await api.revokeAPIKey(id);
        keys = await api.apiKeys();
        toast.success("Key revoked.");
      },
    };
  }

  async function deleteKey(id: number) {
    confirmBox = {
      title: "Permanently delete this key?",
      description: "The key and its usage history disappear. Revoke instead if you might need it back.",
      label: "Delete",
      run: async () => {
        await api.deleteAPIKey(id);
        keys = await api.apiKeys();
        toast.success("Key deleted.");
      },
    };
  }

  function copySecret() {
    navigator.clipboard.writeText(freshSecret);
    toast.success("Secret copied to clipboard.");
  }

  async function reloadUsersTeams() {
    users = await api.users();
    teams = await api.teams();
  }

  async function createUser(e: Event) {
    e.preventDefault();
    try {
      await api.createUser(newUser);
      newUser = { username: "", password: "", role: "member" };
      showNewUser = false;
      await reloadUsersTeams();
      toast.success("User created.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not create user.");
    }
  }

  async function deleteUser(id: number, username: string) {
    confirmBox = {
      title: `Delete user “${username}”?`,
      description: "Their sessions end immediately and their grants disappear.",
      label: "Delete",
      run: async () => {
        await api.deleteUser(id);
        await reloadUsersTeams();
        toast.success("User deleted.");
      },
    };
  }

  async function changeRole(id: number, username: string, role: string) {
    // Demotion is impossible server-side; the select only ever offers
    // promotion, which deserves an explicit confirmation.
    if (role !== "admin") return;
    confirmBox = {
      title: `Promote “${username}” to admin?`,
      description: "Admins bypass every access check and can manage users, teams, keys, and all projects. This cannot be undone by demotion.",
      label: "Promote to admin",
      run: async () => {
        await api.setUserRole(id, role);
        await reloadUsersTeams();
        toast.success("User promoted.");
      },
    };
  }

  async function saveResetPassword(e: Event) {
    e.preventDefault();
    if (!resetPw) return;
    try {
      await api.setUserPassword(resetPw.id, resetPw.password);
      resetPw = null;
      toast.success("Password reset.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not reset password.");
    }
  }

  async function createTeam(e: Event) {
    e.preventDefault();
    try {
      await api.createTeam(newTeam);
      newTeam = { name: "", slug: "" };
      showNewTeam = false;
      await reloadUsersTeams();
      toast.success("Team created.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not create team.");
    }
  }

  async function deleteTeam(id: number, name: string) {
    confirmBox = {
      title: `Delete team “${name}”?`,
      description: "Memberships and project grants through this team disappear. Users keep direct grants.",
      label: "Delete",
      run: async () => {
        await api.deleteTeam(id);
        await reloadUsersTeams();
        toast.success("Team deleted.");
      },
    };
  }

  async function addTeamMember(teamId: number) {
    const userId = teamAddUser[teamId];
    if (!userId) return;
    try {
      await api.addTeamMember(teamId, userId);
      await reloadUsersTeams();
      toast.success("Member added.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not add member.");
    }
  }

  async function removeTeamMember(teamId: number, teamName: string, userId: number) {
    confirmBox = {
      title: `Remove ${usernameOf(userId)} from “${teamName}”?`,
      description: "They lose every project grant they held through this team (direct grants are kept).",
      label: "Remove",
      run: async () => {
        await api.removeTeamMember(teamId, userId);
        await reloadUsersTeams();
        toast.success("Member removed.");
      },
    };
  }

  async function grantTeamProject(teamId: number) {
    const projectId = teamAddProject[teamId];
    if (!projectId) return;
    try {
      await api.grantTeamProject(teamId, projectId);
      await reloadUsersTeams();
      toast.success("Project granted.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not grant project.");
    }
  }

  async function revokeTeamProject(teamId: number, teamName: string, projectId: number) {
    confirmBox = {
      title: `Revoke ${projectName(projectId)} from “${teamName}”?`,
      description: "Every member who held this project only through the team loses access immediately.",
      label: "Revoke",
      run: async () => {
        await api.revokeTeamProject(teamId, projectId);
        await reloadUsersTeams();
        toast.success("Grant revoked.");
      },
    };
  }

  // Single shared confirmation dialog for every destructive action below.
  let confirmBox: { title: string; description: string; label: string; run: () => Promise<void> } | null = null;
  let confirmBusy = false;

  async function runConfirm() {
    if (!confirmBox) return;
    confirmBusy = true;
    try {
      await confirmBox.run();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Action failed.");
    } finally {
      confirmBusy = false;
      confirmBox = null;
    }
  }
</script>

<div class="mx-auto max-w-2xl space-y-4">
  <h2 class="text-2xl font-bold">Settings</h2>
  {#if error}<p class="text-sm text-destructive">{error}</p>{/if}

  <ConfirmDialog
    open={confirmBox !== null}
    title={confirmBox ? confirmBox.title : ""}
    description={confirmBox ? confirmBox.description : ""}
    confirmLabel={confirmBox ? confirmBox.label : ""}
    busy={confirmBusy}
    onConfirm={runConfirm}
    onCancel={() => { if (!confirmBusy) confirmBox = null; }}
  />
  {#if !data}
    <div class="space-y-3" aria-label="Loading settings">
      <div class="h-40 animate-pulse rounded-lg bg-muted"></div>
      <div class="h-40 animate-pulse rounded-lg bg-muted"></div>
    </div>
  {:else}
    <div class="flex flex-col gap-4 md:flex-row">
      <nav aria-label="Settings sections" class="flex shrink-0 gap-1 overflow-x-auto md:w-48 md:flex-col">
        {#each tabs as t}
          <button
            class={"flex items-center gap-2 whitespace-nowrap rounded-md px-3 py-2 text-sm font-medium transition-colors " + (tab === t.id ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground")}
            on:click={() => (tab = t.id)}
          >
            <svelte:component this={t.icon} class="h-4 w-4" />
            {t.label}
          </button>
        {/each}
      </nav>
      <div class="min-w-0 flex-1 space-y-4">
        {#if tab === "general"}
    <Card cls="p-4">
      <form on:submit={saveTracing} class="space-y-3">
        <p class="text-sm font-medium">Trace correlation</p>
        <p class="text-xs text-muted-foreground">URL template with &#123;trace_id&#125; placeholder, e.g. https://tempo.example.com/explore?traceID=&#123;trace_id&#125;</p>
        <Input bind:value={traceURL} placeholder="https://…/&#123;trace_id&#125;" />
        <Button type="submit" size="sm">Save</Button>
      </form>
    </Card>

        {:else if tab === "notifications"}
    <Card cls="p-4">
      <p class="mb-1 text-sm font-medium">Notifications</p>
      <p class="mb-3 text-xs text-muted-foreground">Each destination is configured separately. Alert rules decide which events go where.</p>
      <div class="divide-y divide-border rounded-md border border-border">
        <div class="flex items-center justify-between gap-2 p-3">
          <div class="min-w-0">
            <p class="text-sm font-medium">Telegram {#if data.telegram.enabled}<span class="text-xs text-green-600">(enabled)</span>{/if}</p>
            <p class="truncate font-mono text-xs text-muted-foreground">{telegramStatus()}</p>
          </div>
          <Button size="sm" variant="outline" on:click={() => (channelDialog = "telegram")}>Configure</Button>
        </div>
        {#each channels as c}
          <div class="flex items-center justify-between gap-2 p-3">
            <div class="min-w-0">
              <p class="text-sm font-medium capitalize">{c} {#if data.webhooks[c]?.enabled}<span class="text-xs text-green-600">(enabled)</span>{/if}</p>
              <p class="truncate font-mono text-xs text-muted-foreground">{webhookStatus(c)}</p>
            </div>
            <Button size="sm" variant="outline" on:click={() => (channelDialog = c)}>Configure</Button>
          </div>
        {/each}
        <div class="flex items-center justify-between gap-2 p-3">
          <div class="min-w-0">
            <p class="text-sm font-medium">Email (SMTP) {#if data.smtp.enabled}<span class="text-xs text-green-600">(enabled)</span>{/if}</p>
            <p class="truncate font-mono text-xs text-muted-foreground">{smtpStatus()}</p>
          </div>
          <Button size="sm" variant="outline" on:click={() => (channelDialog = "smtp")}>Configure</Button>
        </div>
        <div class="flex items-center justify-between gap-2 p-3">
          <div class="min-w-0">
            <p class="text-sm font-medium">Generic webhooks</p>
            <p class="truncate font-mono text-xs text-muted-foreground">{generic.length === 0 ? "None yet" : `${generic.length} webhook${generic.length === 1 ? "" : "s"}`}</p>
          </div>
          <Button size="sm" variant="outline" on:click={() => (channelDialog = "generic")}>Configure</Button>
        </div>
      </div>
    </Card>

    <Dialog open={channelDialog === "telegram"} title="Configure Telegram" onClose={closeChannelDialog}>
      <form on:submit={saveTelegram} class="space-y-3">
        <p class="font-mono text-xs text-muted-foreground">Token: {data.botTokenMasked || "(not set)"}</p>
        <div class="space-y-1"><label class="text-xs">Bot token</label><Input type="password" bind:value={telegram.botToken} placeholder="leave blank to keep" /></div>
        <div class="space-y-1"><label class="text-xs">Chat ID</label><Input bind:value={telegram.chatId} /></div>
        <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={telegram.enabled} /> Enabled</label>
        <div class="flex gap-2">
          <Button type="submit" size="sm">Save</Button>
          <Button size="sm" variant="outline" on:click={async () => { const r = await api.testTelegram(); (r.ok ? toast.success(r.message) : toast.error(r.message)); }}>Send test</Button>
        </div>
      </form>
    </Dialog>

    {#each channels as c}
      {#if channelDialog === c}
        <Dialog open={true} title={`Configure ${c}`} onClose={closeChannelDialog}>
          <form on:submit={(e) => saveWebhook(e, c)} class="space-y-3">
            <p class="font-mono text-xs text-muted-foreground">URL: {data.webhooks[c]?.urlMasked || "(not set)"}</p>
            <div class="space-y-1"><label class="text-xs">Webhook URL</label><Input bind:value={webhooks[c].url} placeholder="leave blank to keep" /></div>
            <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={webhooks[c].enabled} /> Enabled</label>
            <div class="flex gap-2">
              <Button type="submit" size="sm">Save</Button>
              <Button size="sm" variant="outline" on:click={async () => { const r = await api.testWebhook(c); (r.ok ? toast.success(r.message) : toast.error(r.message)); }}>Send test</Button>
            </div>
          </form>
        </Dialog>
      {/if}
    {/each}

    <Dialog open={channelDialog === "smtp"} title="Configure Email (SMTP)" onClose={closeChannelDialog}>
      <form on:submit={saveSMTP} class="space-y-3">
        <p class="font-mono text-xs text-muted-foreground">Password: {data.smtp.passwordMasked || "(not set)"}</p>
        <div class="grid grid-cols-2 gap-3">
          <div class="space-y-1"><label class="text-xs">Host</label><Input bind:value={smtp.host} placeholder="smtp.example.com" /></div>
          <div class="space-y-1"><label class="text-xs">Port</label><Input type="number" bind:value={smtp.port} /></div>
          <div class="space-y-1"><label class="text-xs">Username</label><Input bind:value={smtp.username} /></div>
          <div class="space-y-1"><label class="text-xs">Password</label><Input type="password" bind:value={smtp.password} placeholder="leave blank to keep" /></div>
          <div class="space-y-1"><label class="text-xs">From address</label><Input bind:value={smtp.from} placeholder="errgotrack@example.com" /></div>
          <div class="space-y-1"><label class="text-xs">To address (on-call)</label><Input bind:value={smtp.to} placeholder="oncall@example.com" /></div>
        </div>
        <div class="flex gap-4 text-sm">
          <label class="flex items-center gap-2"><input type="checkbox" bind:checked={smtp.useTLS} /> STARTTLS</label>
          <label class="flex items-center gap-2"><input type="checkbox" bind:checked={smtp.enabled} /> Enabled</label>
        </div>
        <div class="flex gap-2">
          <Button type="submit" size="sm">Save</Button>
          <Button size="sm" variant="outline" on:click={async () => { const r = await api.testSMTP(); (r.ok ? toast.success(r.message) : toast.error(r.message)); }}>Send test</Button>
        </div>
      </form>
    </Dialog>

    <Dialog open={channelDialog === "generic"} title="Generic webhooks" onClose={closeChannelDialog}>
      <div class="space-y-3">
        <p class="text-xs text-muted-foreground">Any HTTPS endpoint receives the JSON alert payload. With a secret set, each delivery carries <span class="font-mono">X-Errgotrack-Signature: sha256=&lt;hmac&gt;</span> over the body. Select the <span class="font-mono">generic</span> channel in alert rules to route here.</p>
        {#if generic.length === 0 && !editingHook}
          <p class="text-xs text-muted-foreground">No generic webhooks yet.</p>
        {:else}
          <div class="divide-y divide-border rounded-md border border-border">
            {#each generic as h}
              <div class="flex flex-wrap items-center justify-between gap-2 p-2 text-xs">
                <span>
                  <span class="font-medium">{h.name || "(unnamed)"}</span>
                  {#if h.enabled}<span class="text-green-600"> (enabled)</span>{/if}
                  <span class="block font-mono text-muted-foreground">{h.url}{h.hasSecret ? " · signed" : ""}</span>
                </span>
                <span class="flex gap-2">
                  <Button size="sm" variant="outline" on:click={() => editHook(h)}>Edit</Button>
                  <Button size="sm" variant="outline" on:click={async () => { const r = await api.testGenericWebhook(h.id); (r.ok ? toast.success(r.message) : toast.error(r.message)); }}>Send test</Button>
                  <Button size="sm" variant="outline" on:click={() => deleteHook(h.id)}>Delete</Button>
                </span>
              </div>
            {/each}
          </div>
        {/if}
        {#if editingHook}
          <form on:submit={saveHook} class="space-y-3 rounded-md border border-border p-3">
            <div class="grid grid-cols-2 gap-3">
              <div class="space-y-1"><label class="text-xs">Name</label><Input bind:value={editingHook.name} placeholder="Ops endpoint" /></div>
              <div class="space-y-1"><label class="text-xs">URL</label><Input bind:value={editingHook.url} placeholder="https://ops.example.com/hook" /></div>
              <div class="space-y-1"><label class="text-xs">Secret (blank = keep/unsigned)</label><Input type="password" bind:value={editingHook.secret} placeholder="leave blank to keep" /></div>
              <div class="space-y-1"><label class="text-xs">Extra headers (one "Name: value" per line)</label><textarea rows="3" bind:value={editingHook.headersText} placeholder={"Authorization: Bearer …\nX-Team: ops"} class="flex w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring" /></div>
            </div>
            <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={editingHook.enabled} /> Enabled</label>
            <div class="flex gap-2">
              <Button type="submit" size="sm">Save webhook</Button>
              <Button size="sm" variant="outline" on:click={() => (editingHook = null)}>Cancel</Button>
            </div>
          </form>
        {:else}
          <Button size="sm" variant="outline" on:click={newHook}>New webhook</Button>
        {/if}
      </div>
    </Dialog>
        {:else if tab === "rules"}
    <Card cls="p-4">
      <div class="space-y-3">
        <div class="flex items-center justify-between">
          <p class="text-sm font-medium">Alert rules</p>
          <Button size="sm" variant="outline" on:click={newRule}>New rule</Button>
        </div>
        <p class="text-xs text-muted-foreground">Rules decide which events alert and where. A rule with no channels uses every enabled channel; picking channels routes e.g. production to email and staging to chat.</p>
        {#if rules.length === 0}
          <p class="text-xs text-muted-foreground">No rules yet — nothing will alert.</p>
        {:else}
          <div class="divide-y divide-border rounded-md border border-border">
            {#each rules as r}
              <div class="flex flex-wrap items-center justify-between gap-2 p-2 text-xs">
                <span>
                  <span class="font-medium">{r.name || "(unnamed rule)"}</span>
                  <span class="text-muted-foreground"> · {projectName(r.projectId)}{r.environment ? ` · ${r.environment}` : ""} · cooldown {r.cooldownSeconds}s</span>
                  <span class="block text-muted-foreground">
                    {#if r.notifyNewIssue}new{/if}{#if r.notifyReopened}{r.notifyNewIssue ? " · " : ""}reopened{/if}{#if r.notifyThreshold > 0} · ≥{r.notifyThreshold} events{/if}
                    · → {(r.channels?.length ? r.channels : ["all enabled"]).join(", ")}
                  </span>
                </span>
                <span class="flex gap-2">
                  <Button size="sm" variant="outline" on:click={() => editRule(r)}>Edit</Button>
                  <Button size="sm" variant="outline" on:click={() => deleteRule(r.id)}>Delete</Button>
                </span>
              </div>
            {/each}
          </div>
        {/if}
        {#if editingRule}
          <Dialog open={true} title={editingRule.id ? "Edit alert rule" : "New alert rule"} onClose={() => (editingRule = null)}>
            <form on:submit={saveRule} class="space-y-3">
              <div class="grid grid-cols-2 gap-3">
                <div class="space-y-1"><label class="text-xs">Name</label><Input bind:value={editingRule.name} placeholder="Production → email" /></div>
                <div class="space-y-1"><label class="text-xs">Project</label>
                  <select class="h-9 w-full rounded-md border border-input bg-background px-2 text-sm" bind:value={editingRule.projectId}>
                    <option value={null}>All projects</option>
                    {#each projects as p}<option value={p.id}>{p.name}</option>{/each}
                  </select>
                </div>
                <div class="space-y-1"><label class="text-xs">Environment (blank = all)</label><Input bind:value={editingRule.environment} placeholder="production" /></div>
                <div class="space-y-1"><label class="text-xs">Cooldown (seconds)</label><Input type="number" bind:value={editingRule.cooldownSeconds} /></div>
                <div class="space-y-1"><label class="text-xs">Alert at event count (0 = off)</label><Input type="number" bind:value={editingRule.notifyThreshold} /></div>
              </div>
              <div class="flex gap-4 text-sm">
                <label class="flex items-center gap-2"><input type="checkbox" bind:checked={editingRule.notifyNewIssue} /> New issues</label>
                <label class="flex items-center gap-2"><input type="checkbox" bind:checked={editingRule.notifyReopened} /> Reopened</label>
              </div>
              <div class="space-y-1">
                <p class="text-xs">Channels (none = all enabled)</p>
                <div class="flex flex-wrap gap-3 text-sm">
                  {#each allChannels as c}
                    <label class="flex items-center gap-1.5"><input type="checkbox" checked={editingRule.channels?.includes(c)} on:change={() => toggleRuleChannel(c)} /> {c}</label>
                  {/each}
                </div>
              </div>
              <div class="flex gap-2">
                <Button type="submit" size="sm">Save rule</Button>
                <Button size="sm" variant="outline" on:click={() => (editingRule = null)}>Cancel</Button>
              </div>
            </form>
          </Dialog>
        {/if}
      </div>
    </Card>
        {:else if tab === "keys"}
    <Card cls="p-4">
      <div class="space-y-3">
        <p class="text-sm font-medium">API keys</p>
        <p class="text-xs text-muted-foreground">Bearer tokens for automation: <span class="font-mono">ingest</span> posts events without a DSN, <span class="font-mono">read</span>/<span class="font-mono">write</span> use the management API without a session. Optionally bound to one project and with an expiry. Secrets are shown once — only hashes are stored.</p>
        {#if freshSecret}
          <div class="space-y-2 rounded-md border border-border bg-muted p-3">
            <p class="text-xs font-medium">Copy this secret now — it will never be shown again:</p>
            <code class="block overflow-x-auto font-mono text-xs">{freshSecret}</code>
            <div class="flex gap-2">
              <Button size="sm" variant="outline" on:click={copySecret}>Copy</Button>
              <Button size="sm" variant="outline" on:click={() => (freshSecret = "")}>Done</Button>
            </div>
          </div>
        {/if}
        {#if keys.length === 0}
          <p class="text-xs text-muted-foreground">No API keys yet.</p>
        {:else}
          <div class="divide-y divide-border rounded-md border border-border">
            {#each keys as k}
              <div class="flex flex-wrap items-center justify-between gap-2 p-2 text-xs">
                <span>
                  <span class="font-medium">{k.name || "(unnamed)"}</span>
                  {#if k.revoked}<span class="text-destructive"> · revoked</span>{/if}
                  <span class="block font-mono text-muted-foreground">{k.prefix}… · {k.scopes.join(", ")} · {k.projectId === null ? "all projects" : projectName(k.projectId)}{k.expiresAt ? ` · expires ${k.expiresAt}` : ""}{k.lastUsedAt ? ` · used ${k.lastUsedAt}` : " · never used"}</span>
                </span>
                <span class="flex gap-2">
                  {#if !k.revoked}
                    <Button size="sm" variant="outline" on:click={() => rotateKey(k.id)}>Rotate</Button>
                    <Button size="sm" variant="outline" on:click={() => revokeKey(k.id)}>Revoke</Button>
                  {/if}
                  <Button size="sm" variant="outline" on:click={() => deleteKey(k.id)}>Delete</Button>
                </span>
              </div>
            {/each}
          </div>
        {/if}
        <Button size="sm" variant="outline" on:click={() => (showNewKey = true)}>New key</Button>
        {#if showNewKey}
          <Dialog open={true} title="New API key" onClose={() => (showNewKey = false)}>
            <form on:submit={createKey} class="space-y-3">
              <div class="grid grid-cols-2 gap-3">
                <div class="space-y-1"><label class="text-xs">Name</label><Input bind:value={newKey.name} placeholder="CI deploy key" /></div>
                <div class="space-y-1"><label class="text-xs">Project</label>
                  <select class="h-9 w-full rounded-md border border-input bg-background px-2 text-sm" bind:value={newKey.projectId}>
                    <option value={null}>All projects</option>
                    {#each projects as p}<option value={p.id}>{p.name}</option>{/each}
                  </select>
                </div>
                <div class="space-y-1"><label class="text-xs">Expires (optional, YYYY-MM-DD)</label><Input bind:value={newKey.expiresAt} placeholder="" /></div>
              </div>
              <div class="space-y-1">
                <p class="text-xs">Scopes</p>
                <div class="flex flex-wrap gap-3 text-sm">
                  {#each allScopes as s}
                    <label class="flex items-center gap-1.5"><input type="checkbox" checked={newKey.scopes.includes(s)} on:change={() => toggleScope(s)} /> {s}</label>
                  {/each}
                </div>
              </div>
              <div class="flex gap-2">
                <Button type="submit" size="sm">Create key</Button>
                <Button size="sm" variant="outline" on:click={() => (showNewKey = false)}>Cancel</Button>
              </div>
            </form>
          </Dialog>
        {/if}
      </div>
    </Card>
        {:else if tab === "users"}
    <Card cls="p-4">
      <div class="space-y-4">
        <div>
          <p class="text-sm font-medium">Users</p>
          <p class="text-xs text-muted-foreground">Admins bypass every check. Members only see projects granted to them directly or through a team.</p>
        </div>
        {#if users.length}
          <div class="divide-y divide-border rounded-md border border-border">
            {#each users as u}
              {@const isSelf = u.username === meUsername}
              <div class="flex flex-wrap items-center justify-between gap-2 p-2 text-xs">
                <span>
                  <span class="font-medium">{u.username}</span>
                  {#if isSelf}<span class="text-muted-foreground"> (you)</span>{/if}
                  <span class="text-muted-foreground"> · {u.role}</span>
                </span>
                <span class="flex items-center gap-2">
                  {#if u.role === "admin"}
                    <span class="text-xs text-muted-foreground" title="Admins cannot be demoted">admin</span>
                  {:else}
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={isSelf}
                      title={isSelf ? "You cannot change your own role" : "Promote to admin"}
                      on:click={() => changeRole(u.id, u.username, "admin")}
                    >Promote</Button>
                  {/if}
                  <Button size="sm" variant="outline" on:click={() => (resetPw = { id: u.id, username: u.username, password: "" })}>Reset password</Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={isSelf}
                    title={isSelf ? "You cannot delete your own account" : "Delete user"}
                    on:click={() => deleteUser(u.id, u.username)}
                  >Delete</Button>
                </span>
              </div>
            {/each}
          </div>
        {:else}
          <p class="text-xs text-muted-foreground">No users yet.</p>
        {/if}
        <Button size="sm" variant="outline" on:click={() => (showNewUser = true)}>New user</Button>
        {#if showNewUser}
          <Dialog open={true} title="New user" onClose={() => (showNewUser = false)}>
            <form on:submit={createUser} class="space-y-3">
              <div class="grid grid-cols-2 gap-3">
                <div class="space-y-1"><label class="text-xs">Username</label><Input bind:value={newUser.username} placeholder="jane" autocomplete="off" /></div>
                <div class="space-y-1"><label class="text-xs">Password (min 8 chars)</label><Input type="password" bind:value={newUser.password} placeholder="••••••••" autocomplete="new-password" /></div>
                <div class="space-y-1"><label class="text-xs">Role</label>
                  <select class="h-9 w-full rounded-md border border-input bg-background px-2 text-sm" bind:value={newUser.role}>
                    <option value="member">member</option>
                    <option value="admin">admin</option>
                  </select>
                </div>
              </div>
              <div class="flex gap-2">
                <Button type="submit" size="sm">Create user</Button>
                <Button size="sm" variant="outline" on:click={() => (showNewUser = false)}>Cancel</Button>
              </div>
            </form>
          </Dialog>
        {/if}
      </div>
    </Card>
    <Card cls="p-4">
      <div class="space-y-4">
        <div>
          <p class="text-sm font-medium">Teams</p>
          <p class="text-xs text-muted-foreground">Group users, then grant whole teams to projects. A member sees a project with a direct grant or any team grant.</p>
        </div>
        {#if teams.length}
          <div class="space-y-3">
            {#each teams as t}
              <div class="rounded-md border border-border p-3">
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <p class="text-sm font-medium">{t.name} <span class="font-mono text-xs text-muted-foreground">{t.slug}</span></p>
                  <Button size="sm" variant="outline" on:click={() => deleteTeam(t.id, t.name)}>Delete</Button>
                </div>
                <div class="mt-2">
                  <p class="text-xs text-muted-foreground">Members ({t.memberIds.length})</p>
                  <div class="mt-1 flex flex-wrap gap-1">
                    {#each t.memberIds as uid}
                      <span class="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-xs">
                        {usernameOf(uid)}
                        <button class="text-muted-foreground hover:text-foreground" aria-label="Remove {usernameOf(uid)}" on:click={() => removeTeamMember(t.id, t.name, uid)}>✕</button>
                      </span>
                    {:else}
                      <span class="text-xs text-muted-foreground">No members yet.</span>
                    {/each}
                  </div>
                  <div class="mt-2 flex gap-2">
                    <select class="h-8 flex-1 rounded-md border border-input bg-background px-2 text-xs" bind:value={teamAddUser[t.id]}>
                      <option value={null}>Add member…</option>
                      {#each users.filter((u) => !t.memberIds.includes(u.id)) as u}<option value={u.id}>{u.username}</option>{/each}
                    </select>
                    <Button size="sm" variant="outline" on:click={() => addTeamMember(t.id)}>Add</Button>
                  </div>
                </div>
                <div class="mt-2">
                  <p class="text-xs text-muted-foreground">Projects ({t.projectIds.length})</p>
                  <div class="mt-1 flex flex-wrap gap-1">
                    {#each t.projectIds as pid}
                      <span class="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-xs">
                        {projectName(pid)}
                        <button class="text-muted-foreground hover:text-foreground" aria-label="Revoke {projectName(pid)}" on:click={() => revokeTeamProject(t.id, t.name, pid)}>✕</button>
                      </span>
                    {:else}
                      <span class="text-xs text-muted-foreground">No project grants yet.</span>
                    {/each}
                  </div>
                  <div class="mt-2 flex gap-2">
                    <select class="h-8 flex-1 rounded-md border border-input bg-background px-2 text-xs" bind:value={teamAddProject[t.id]}>
                      <option value={null}>Grant project…</option>
                      {#each projects.filter((p) => !t.projectIds.includes(p.id)) as p}<option value={p.id}>{p.name}</option>{/each}
                    </select>
                    <Button size="sm" variant="outline" on:click={() => grantTeamProject(t.id)}>Grant</Button>
                  </div>
                </div>
              </div>
            {/each}
          </div>
        {:else}
          <p class="text-xs text-muted-foreground">No teams yet.</p>
        {/if}
        <Button size="sm" variant="outline" on:click={() => (showNewTeam = true)}>New team</Button>
        {#if showNewTeam}
          <Dialog open={true} title="New team" onClose={() => (showNewTeam = false)}>
            <form on:submit={createTeam} class="space-y-3">
              <div class="grid grid-cols-2 gap-3">
                <div class="space-y-1"><label class="text-xs">Name</label><Input bind:value={newTeam.name} placeholder="Backend" autocomplete="off" /></div>
                <div class="space-y-1"><label class="text-xs">Slug (optional)</label><Input bind:value={newTeam.slug} placeholder="backend" autocomplete="off" /></div>
              </div>
              <div class="flex gap-2">
                <Button type="submit" size="sm">Create team</Button>
                <Button size="sm" variant="outline" on:click={() => (showNewTeam = false)}>Cancel</Button>
              </div>
            </form>
          </Dialog>
        {/if}
      </div>
    </Card>
    {#if resetPw}
      <Dialog open={true} title={`Reset password for ${resetPw.username}`} onClose={() => (resetPw = null)}>
        <form on:submit={saveResetPassword} class="space-y-3">
          <div class="space-y-1"><label class="text-xs">New password (min 8 chars)</label><Input type="password" bind:value={resetPw.password} autocomplete="new-password" /></div>
          <div class="flex gap-2">
            <Button type="submit" size="sm">Save password</Button>
            <Button size="sm" variant="outline" on:click={() => (resetPw = null)}>Cancel</Button>
          </div>
        </form>
      </Dialog>
    {/if}
        {:else if tab === "storage"}
    <Card cls="p-4">
      <div class="space-y-3">
        <p class="text-sm font-medium">Storage</p>
        {#if !storage}
          <p class="text-xs text-muted-foreground">Loading usage…</p>
        {:else}
          <p class="text-xs text-muted-foreground">
            Backend: <span class="font-mono">{storage.driver}</span>
            · Database size: <span class="font-mono">{formatBytes(storage.sizeBytes)}</span>
          </p>
          <div class="grid grid-cols-2 gap-1 text-xs sm:grid-cols-3">
            {#each storage.tables as t}
              <div class="flex justify-between gap-2 rounded bg-muted px-2 py-1">
                <span class="font-mono text-muted-foreground">{t.name}</span>
                <span class="font-mono">{t.rows.toLocaleString()}</span>
              </div>
            {/each}
          </div>
        {/if}
        <p class="text-xs text-muted-foreground">The retention sweep deletes events older than each project's retention window (issue summaries are kept). Vacuum reclaims free space afterwards.</p>
        <div class="flex gap-2">
          <Button size="sm" variant="outline" disabled={storageBusy !== ""} on:click={runSweep}>
            {storageBusy === "sweep" ? "Sweeping…" : "Run retention sweep now"}
          </Button>
          <Button size="sm" variant="outline" disabled={storageBusy !== ""} on:click={runVacuum}>
            {storageBusy === "vacuum" ? "Vacuuming…" : "Vacuum database"}
          </Button>
        </div>
      </div>
    </Card>
        {/if}
      </div>
    </div>
  {/if}
</div>
