<script lang="ts">
  import { onMount } from "svelte";
  import { api, type SettingsData, type StorageUsage, type AlertRule, type AlertRuleInput, type APIKey, type Integration, type IntegrationInput, type RoutingOverride, type User, type Team, type SecurityPolicy, type AuditLogEntry, type QuotaDefaults, type SSOSettings } from "$lib/api";
  import * as CardUI from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import { Root as Skeleton } from "$lib/components/ui/skeleton/index.js";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import { toast } from "svelte-sonner";
  import ConfirmDialog from "$lib/components/ConfirmDialog.svelte";
  import Dialog from "$lib/components/Dialog.svelte";
  import { Bell, KeyRound, Database, SlidersHorizontal, ScrollText, Users, TriangleAlert, ShieldCheck, History } from "lucide-svelte";

  let tab = "general";
  const tabs = [
    { id: "general", label: "General", icon: SlidersHorizontal },
    { id: "notifications", label: "Notifications", icon: Bell },
    { id: "rules", label: "Alert rules", icon: ScrollText },
    { id: "keys", label: "API keys", icon: KeyRound },
    { id: "users", label: "Users & Teams", icon: Users },
    { id: "audit", label: "Audit log", icon: History },
    { id: "storage", label: "Storage", icon: Database },
  ];

  let data: SettingsData | null = null;
  let error = "";
  let integrations: Integration[] = [];
  let editingIntegration: { id?: number; name: string; provider: string; enabled: boolean; creds: Record<string, any> } | null = null;
  let traceURL = "";
  let storage: StorageUsage | null = null;
  let storageBusy = "";
  let rules: AlertRule[] = [];
  let projects: { id: number; name: string }[] = [];
  let editingRule: (AlertRuleInput & { id?: number }) | null = null;
  let keys: APIKey[] = [];
  let newKey = { name: "", projectId: null as number | null, scopes: ["read"] as string[], expiresAt: "" };
  let freshSecret = "";
  let users: User[] = [];
  let teams: Team[] = [];
  let newUser = { username: "", password: "", role: "member", email: "" };
  let newTeam = { name: "", slug: "" };
  let showNewUser = false;
  let showNewTeam = false;
  let showNewKey = false;
  let meUsername = "";
  let security: SecurityPolicy = { require2fa: false };
  let quotaDefaults: QuotaDefaults = { defaultHourly: 5000, defaultDaily: 50000 };
  let ssoSettings: SSOSettings = { enabled: false, name: "", issuer: "", passwordLoginDisabled: false };
  let auditEntries: AuditLogEntry[] = [];
  let auditTotal = 0;
  let auditOffset = 0;
  let auditFilter = "";
  let auditBusy = false;
  const auditPageSize = 50;

  async function loadAudit() {
    auditBusy = true;
    try {
      const res = await api.auditLog({ action: auditFilter.trim() || undefined, limit: auditPageSize, offset: auditOffset });
      auditEntries = res.entries;
      auditTotal = res.total;
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not load audit log.");
    } finally {
      auditBusy = false;
    }
  }

  function auditPage(delta: number) {
    auditOffset = Math.max(0, auditOffset + delta * auditPageSize);
    loadAudit();
  }
  let resetPw: { id: number; username: string; password: string } | null = null;
  let teamAddUser: Record<number, number | null> = {};
  let teamAddProject: Record<number, number | null> = {};

  function usernameOf(id: number): string {
    return users.find((u) => u.id === id)?.username ?? `User ${id}`;
  }

  const allScopes = ["ingest", "read", "write"];
  const providers = ["telegram", "slack", "discord", "teams", "sheets", "smtp", "generic"];

  function credSummary(i: Integration): string {
    const c = (i.credentials ?? {}) as Record<string, any>;
    switch (i.provider) {
      case "telegram": return String(c.default_chat_id ?? "");
      case "slack": case "discord": case "teams": case "sheets": case "generic": return String(c.url ?? "");
      case "smtp": return String(c.host ?? "");
      default: return "";
    }
  }

  function newIntegration() {
    editingIntegration = { name: "", provider: "telegram", enabled: true, creds: {} };
  }

  function editIntegration(i: Integration) {
    editingIntegration = { id: i.id, name: i.name, provider: i.provider, enabled: i.enabled, creds: { ...((i.credentials ?? {}) as Record<string, any>) } };
  }

  async function saveIntegration(e: Event) {
    e.preventDefault();
    if (!editingIntegration) return;
    const input: IntegrationInput = { name: editingIntegration.name, provider: editingIntegration.provider, enabled: editingIntegration.enabled, credentials: editingIntegration.creds };
    try {
      if (editingIntegration.id) {
        await api.updateIntegration(editingIntegration.id, input);
        toast.success("Integration updated.");
      } else {
        await api.createIntegration(input);
        toast.success("Integration created.");
      }
      editingIntegration = null;
      integrations = await api.integrations();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not save integration.");
    }
  }

  async function deleteIntegration(id: number) {
    confirmBox = {
      title: "Delete this integration?",
      description: "Rules linked to it will skip the deleted destination.",
      label: "Delete",
      run: async () => {
        await api.deleteIntegration(id);
        integrations = await api.integrations();
        rules = await api.rules();
        toast.success("Integration deleted.");
      },
    };
  }

  async function testIntegration(id: number, override?: RoutingOverride) {
    try {
      const r = await api.testIntegration(id, override);
      if (r.ok) toast.success(r.message);
      else toast.error(r.message);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Test failed.");
    }
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
      toast.success(`Retention sweep cleaned ${r.deleted} row(s) (events, logs, traces).`);
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
      traceURL = data.traceURLTemplate;
      await loadStorage();
      integrations = await api.integrations();
      rules = await api.rules();
      projects = await api.projects();
      keys = await api.apiKeys();
      try {
        users = await api.users();
        teams = await api.teams();
        security = await api.security();
        quotaDefaults = await api.quotaDefaults();
        ssoSettings = await api.ssoSettings();
        meUsername = (await api.me()).username ?? "";
      } catch (e) {
        toast.error(`Could not load users/teams: ${e instanceof Error ? e.message : e}`);
      }
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load settings.";
    }
  });

  async function saveTracing(e: Event) {
    e.preventDefault();
    data = await api.saveTracing(traceURL);
    toast.success("Tracing settings saved.");
  }

  function projectName(pid: number | null): string {
    if (pid === null || pid === undefined) return "All projects";
    return projects.find((p) => p.id === pid)?.name ?? `Project ${pid}`;
  }

  function ruleLinkLabel(l: { integrationId: number; integrationName?: string; provider?: string }): string {
    return l.integrationName ? `${l.integrationName} (${l.provider})` : `integration ${l.integrationId}`;
  }

  function getOverride(id: number, field: string): string {
    const o = ruleOverride(id) as unknown as Record<string, unknown>;
    const v = o[field];
    return typeof v === "string" ? v : "";
  }

  function setOverride(id: number, field: string, value: string) {
    const o = ruleOverride(id) as unknown as Record<string, unknown>;
    o[field] = value;
  }

  function newRule() {
    editingRule = { name: "", projectId: null, environment: "", notifyNewIssue: true, notifyThreshold: 0, notifyReopened: true, cooldownSeconds: 300, integrations: [] };
  }

  function editRule(r: AlertRule) {
    editingRule = { id: r.id, name: r.name, projectId: r.projectId, environment: r.environment, notifyNewIssue: r.notifyNewIssue, notifyThreshold: r.notifyThreshold, notifyReopened: r.notifyReopened, cooldownSeconds: r.cooldownSeconds, integrations: (r.integrations ?? []).map((l) => ({ integrationId: l.integrationId, routingOverride: { ...(l.routingOverride ?? {}) } })) };
  }

  async function saveRule(e: Event) {
    e.preventDefault();
    if (!editingRule) return;
    normalizeIntegrations();
    try {
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
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not save rule.");
    }
  }

  async function deleteRule(id: number) {
    confirmBox = {
      title: "Delete this alert rule?",
      description: "Matching events will stop notifying on the rule's integrations.",
      label: "Delete",
      run: async () => {
        await api.deleteRule(id);
        rules = await api.rules();
        toast.success("Rule deleted.");
      },
    };
  }

  function toggleRuleIntegration(id: number) {
    if (!editingRule) return;
    const list = editingRule.integrations ?? [];
    if (list.some((l) => l.integrationId === id)) {
      editingRule.integrations = list.filter((l) => l.integrationId !== id);
    } else {
      editingRule.integrations = [...list, { integrationId: id, routingOverride: {} }];
    }
  }

  function ruleOverride(id: number): RoutingOverride {
    const l = (editingRule?.integrations ?? []).find((x) => x.integrationId === id);
    if (!l) return {};
    if (!l.routingOverride) l.routingOverride = {};
    return l.routingOverride;
  }

  function toString(id: number): string {
    const v = ruleOverride(id).to;
    return Array.isArray(v) ? v.join(", ") : (v ?? "");
  }

  function setToString(id: number, s: string) {
    const o = ruleOverride(id);
    o.to = s.split(",").map((x) => x.trim()).filter(Boolean);
  }

  // Normalize UI string inputs into API shape before save (smtp to may come back as string).
  function normalizeIntegrations() {
    if (!editingRule?.integrations) return;
    for (const l of editingRule.integrations) {
      const o = l.routingOverride as unknown as Record<string, unknown>;
      if (typeof o.to === "string") {
        o.to = (o.to as string).split(",").map((x: string) => x.trim()).filter(Boolean);
      }
    }
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
      newUser = { username: "", password: "", role: "member", email: "" };
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

  function resetUser2fa(id: number, username: string) {
    confirmBox = {
      title: `Reset two-factor authentication for “${username}”?`,
      description: "Their authenticator and all recovery codes stop working immediately. At their next sign-in they set up a fresh second factor.",
      label: "Reset 2FA",
      run: async () => {
        await api.resetUser2fa(id);
        await reloadUsersTeams();
        toast.success("Two-factor authentication reset.");
      },
    };
  }

  async function saveSecurity(e: Event) {
    e.preventDefault();
    try {
      security = await api.saveSecurity(security.require2fa);
      toast.success(security.require2fa ? "Two-factor authentication is now required for every login." : "Two-factor enforcement disabled.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not save security settings.");
    }
  }

  async function saveQuotaDefaults(e: Event) {
    e.preventDefault();
    try {
      quotaDefaults = await api.saveQuotaDefaults(quotaDefaults);
      toast.success("Default quotas saved. Projects without overrides inherit them.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not save quotas.");
    }
  }

  async function saveSSO(e: Event) {
    e.preventDefault();
    if (ssoSettings.passwordLoginDisabled && !ssoSettings.enabled) return;
    try {
      ssoSettings = await api.saveSSOSettings(ssoSettings.passwordLoginDisabled);
      toast.success(ssoSettings.passwordLoginDisabled ? "Password login disabled — sign-in is SSO-only." : "Password login enabled.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not save SSO settings.");
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

<div class="mx-auto max-w-2xl space-y-6">
  <div>
    <h1 class="text-2xl font-semibold tracking-tight">Settings</h1>
    <p class="text-muted-foreground mt-1 text-sm">Instance configuration and access</p>
  </div>
  {#if error}
    <Alert.Root variant="destructive">
      <TriangleAlert />
      <Alert.Title>Couldn't load settings</Alert.Title>
      <Alert.Description>{error}</Alert.Description>
    </Alert.Root>
  {/if}

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
    <CardUI.Root>
      <CardUI.Content class="space-y-2 pt-6" aria-label="Loading settings">
        <Skeleton class="h-40 w-full" />
        <Skeleton class="h-40 w-full" />
      </CardUI.Content>
    </CardUI.Root>
  {:else}
    <div class="flex flex-col gap-4 md:flex-row">
      <nav aria-label="Settings sections" class="flex shrink-0 gap-1 overflow-x-auto md:w-48 md:flex-col">
        {#each tabs as t}
          <button
            class={"flex items-center gap-2 whitespace-nowrap rounded-md px-3 py-2 text-sm font-medium transition-colors " + (tab === t.id ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground")}
            on:click={() => { tab = t.id; if (t.id === "audit") { auditOffset = 0; loadAudit(); } }}
          >
            <svelte:component this={t.icon} class="h-4 w-4" />
            {t.label}
          </button>
        {/each}
      </nav>
      <div class="min-w-0 flex-1 space-y-4">
        {#if tab === "general"}
    <CardUI.Root class="gap-0">
      <CardUI.Header>
        <CardUI.Title class="flex items-center gap-2"><ShieldCheck class="size-4" /> Two-factor enforcement</CardUI.Title>
        <CardUI.Description>When required, every password sign-in continues to an authenticator code, and users without a factor enroll one before their session is issued. API keys are unaffected.</CardUI.Description>
      </CardUI.Header>
      <CardUI.Content>
      <form on:submit={saveSecurity} class="flex flex-wrap items-center gap-3">
        <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={security.require2fa} /> Require two-factor authentication for all logins</label>
        <Button type="submit" size="sm">Save</Button>
      </form>
      </CardUI.Content>
    </CardUI.Root>
    <CardUI.Root class="gap-0">
      <CardUI.Header>
        <CardUI.Title>Default ingestion quotas</CardUI.Title>
        <CardUI.Description>Spike protection inherited by projects without overrides. 0 disables a window. Counts stored error events over trailing hour/day windows.</CardUI.Description>
      </CardUI.Header>
      <CardUI.Content>
      <form on:submit={saveQuotaDefaults} class="flex flex-wrap items-end gap-3">
        <div class="space-y-1"><label class="text-xs">Events per hour (0 = unlimited)</label><Input type="number" min="0" bind:value={quotaDefaults.defaultHourly} /></div>
        <div class="space-y-1"><label class="text-xs">Events per day (0 = unlimited)</label><Input type="number" min="0" bind:value={quotaDefaults.defaultDaily} /></div>
        <Button type="submit" size="sm">Save</Button>
      </form>
      </CardUI.Content>
    </CardUI.Root>
    <CardUI.Root class="gap-0">
      <CardUI.Header>
        <CardUI.Title>Single sign-on</CardUI.Title>
        <CardUI.Description>
          {#if ssoSettings.enabled}
            {ssoSettings.name} · {ssoSettings.issuer}. First login auto-provisions a member; matching emails link existing accounts.
          {:else}
            Not configured. Set SPLAT_OIDC_ISSUER, SPLAT_OIDC_CLIENT_ID, and SPLAT_OIDC_CLIENT_SECRET, then restart.
          {/if}
        </CardUI.Description>
      </CardUI.Header>
      <CardUI.Content>
      {#if ssoSettings.enabled}
        <form on:submit={saveSSO} class="flex flex-wrap items-center gap-3">
          <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={ssoSettings.passwordLoginDisabled} /> Disable password login (SSO-only sign-in)</label>
          <Button type="submit" size="sm">Save</Button>
        </form>
      {:else}
        <p class="text-xs text-muted-foreground">Password login cannot be disabled until a provider is configured — that combination would lock everyone out.</p>
      {/if}
      </CardUI.Content>
    </CardUI.Root>
    <CardUI.Root class="gap-0">
      <CardUI.Header>
        <CardUI.Title>Trace correlation</CardUI.Title>
        <CardUI.Description>URL template with &#123;trace_id&#125; placeholder, e.g. https://tempo.example.com/explore?traceID=&#123;trace_id&#125;</CardUI.Description>
      </CardUI.Header>
      <CardUI.Content>
      <form on:submit={saveTracing} class="space-y-3">
        <Input bind:value={traceURL} placeholder="https://…/&#123;trace_id&#125;" />
        <Button type="submit" size="sm">Save</Button>
      </form>
      </CardUI.Content>
    </CardUI.Root>

        {:else if tab === "notifications"}
    <CardUI.Root class="gap-0">
      <CardUI.Header class="flex-row items-start justify-between gap-2">
        <div>
          <CardUI.Title>Integrations</CardUI.Title>
          <CardUI.Description>Named destinations. Create e.g. one Telegram integration per project/channel, then link them in alert rules.</CardUI.Description>
        </div>
        <Button size="sm" variant="outline" class="shrink-0" on:click={newIntegration}>New integration</Button>
      </CardUI.Header>
      <CardUI.Content>
      <div class="space-y-3">
        {#if integrations.length === 0}
          <p class="text-xs text-muted-foreground">No integrations yet — create one to start alerting.</p>
        {:else}
          <div class="divide-y divide-border rounded-md border border-border">
            {#each integrations as i}
              <div class="flex flex-wrap items-center justify-between gap-2 p-2 text-xs">
                <span>
                  <span class="font-medium">{i.name || "(unnamed)"}</span>
                  <span class="text-muted-foreground"> · {i.provider}</span>
                  {#if i.enabled}<span class="text-green-600"> · enabled</span>{:else}<span class="text-muted-foreground"> · disabled</span>{/if}
                  {#if i.failureCount > 0}<span class="text-destructive"> · {i.failureCount} failures</span>{/if}
                  <span class="block font-mono text-muted-foreground">{credSummary(i)}</span>
                </span>
                <span class="flex gap-2">
                  <Button size="sm" variant="outline" on:click={() => editIntegration(i)}>Edit</Button>
                  <Button size="sm" variant="outline" on:click={() => testIntegration(i.id)}>Send test</Button>
                  <Button size="sm" variant="outline" on:click={() => deleteIntegration(i.id)}>Delete</Button>
                </span>
              </div>
            {/each}
          </div>
        {/if}
        {#if editingIntegration}
          <Dialog open={true} title={editingIntegration.id ? "Edit integration" : "New integration"} onClose={() => (editingIntegration = null)}>
            <form on:submit={saveIntegration} class="space-y-3">
              <div class="grid grid-cols-2 gap-3">
                <div class="space-y-1"><label class="text-xs">Name</label><Input bind:value={editingIntegration.name} placeholder="Team A Telegram" /></div>
                <div class="space-y-1"><label class="text-xs">Provider</label>
                  <select class="h-9 w-full rounded-md border border-input bg-background px-2 text-sm" bind:value={editingIntegration.provider}>
                    {#each providers as pv}<option value={pv}>{pv}</option>{/each}
                  </select>
                </div>
              </div>
              {#if editingIntegration.provider === "telegram"}
                <div class="space-y-1"><label class="text-xs">Bot token (blank keeps stored)</label><Input type="password" bind:value={editingIntegration.creds.bot_token} placeholder="leave blank to keep" /></div>
                <div class="space-y-1"><label class="text-xs">Default chat ID</label><Input bind:value={editingIntegration.creds.default_chat_id} placeholder="-100123…" /></div>
              {:else if editingIntegration.provider === "smtp"}
                <div class="grid grid-cols-2 gap-3">
                  <div class="space-y-1"><label class="text-xs">Host</label><Input bind:value={editingIntegration.creds.host} placeholder="smtp.example.com" /></div>
                  <div class="space-y-1"><label class="text-xs">Port</label><Input type="number" bind:value={editingIntegration.creds.port} /></div>
                  <div class="space-y-1"><label class="text-xs">Username</label><Input bind:value={editingIntegration.creds.username} /></div>
                  <div class="space-y-1"><label class="text-xs">Password (blank keeps)</label><Input type="password" bind:value={editingIntegration.creds.password} placeholder="leave blank to keep" /></div>
                  <div class="space-y-1"><label class="text-xs">From address</label><Input bind:value={editingIntegration.creds.from} placeholder="splat@example.com" /></div>
                </div>
              {:else if editingIntegration.provider === "generic"}
                <div class="space-y-1"><label class="text-xs">URL</label><Input bind:value={editingIntegration.creds.url} placeholder="https://ops.example.com/hook" /></div>
                <div class="space-y-1"><label class="text-xs">Secret (blank keeps/unsigned)</label><Input type="password" bind:value={editingIntegration.creds.secret} placeholder="leave blank to keep" /></div>
                <p class="text-xs text-muted-foreground">Custom headers can be set per rule via extra_headers.</p>
              {:else}
                <div class="space-y-1"><label class="text-xs">Webhook URL</label><Input bind:value={editingIntegration.creds.url} placeholder="https://…" /></div>
              {/if}
              <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={editingIntegration.enabled} /> Enabled</label>
              <div class="flex gap-2">
                <Button type="submit" size="sm">Save integration</Button>
                <Button size="sm" variant="outline" on:click={() => (editingIntegration = null)}>Cancel</Button>
              </div>
            </form>
          </Dialog>
        {/if}
      </div>
      </CardUI.Content>
    </CardUI.Root>
        {:else if tab === "rules"}
    <CardUI.Root class="gap-0">
      <CardUI.Header class="flex-row items-start justify-between gap-2">
        <div>
          <CardUI.Title>Alert rules</CardUI.Title>
          <CardUI.Description>Rules decide which events alert and where. Link one or more integrations per rule; each link can override the destination.</CardUI.Description>
        </div>
        <Button size="sm" variant="outline" class="shrink-0" on:click={newRule}>New rule</Button>
      </CardUI.Header>
      <CardUI.Content>
      <div class="space-y-3">
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
                    · → {(r.integrations?.length ? r.integrations.map(ruleLinkLabel) : ["no destinations"]).join(", ")}
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
                <div class="space-y-1"><label class="text-xs">Name</label><Input bind:value={editingRule.name} placeholder="Production → Team A" /></div>
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
              <div class="space-y-2">
                <p class="text-xs">Integrations</p>
                {#if integrations.length === 0}
                  <p class="text-xs text-muted-foreground">No integrations yet — create one under Notifications first.</p>
                {:else}
                  <div class="space-y-2">
                    {#each integrations as i}
                      <div class="rounded-md border border-border p-2">
                        <label class="flex items-center gap-2 text-sm">
                          <input type="checkbox" checked={(editingRule.integrations ?? []).some((l) => l.integrationId === i.id)} on:change={() => toggleRuleIntegration(i.id)} />
                          <span class="font-medium">{i.name}</span><span class="text-muted-foreground">· {i.provider}{i.enabled ? "" : " (disabled)"}</span>
                        </label>
                        {#if (editingRule.integrations ?? []).some((l) => l.integrationId === i.id)}
                          <div class="mt-2 grid grid-cols-2 gap-2">
                            {#if i.provider === "telegram"}
                              <div class="space-y-1"><label class="text-xs">Chat ID override (blank = default)</label><Input value={getOverride(i.id, "chat_id")} on:input={(e) => setOverride(i.id, "chat_id", (e.target as HTMLInputElement).value)} placeholder={String((i.credentials?.default_chat_id ?? "") || "default")} /></div>
                            {:else if i.provider === "smtp"}
                              <div class="space-y-1 col-span-2"><label class="text-xs">Recipients (comma-separated)</label><Input value={toString(i.id)} on:input={(e) => setToString(i.id, (e.target as HTMLInputElement).value)} placeholder="ops@example.com" /></div>
                            {:else if i.provider === "generic"}
                              <div class="space-y-1"><label class="text-xs">URL override (blank = default)</label><Input value={getOverride(i.id, "url")} on:input={(e) => setOverride(i.id, "url", (e.target as HTMLInputElement).value)} placeholder="default" /></div>
                            {:else}
                              <div class="space-y-1"><label class="text-xs">URL override (blank = default)</label><Input value={getOverride(i.id, "url")} on:input={(e) => setOverride(i.id, "url", (e.target as HTMLInputElement).value)} placeholder="default" /></div>
                            {/if}
                          </div>
                        {/if}
                      </div>
                    {/each}
                  </div>
                {/if}
              </div>
              <div class="flex gap-2">
                <Button type="submit" size="sm">Save rule</Button>
                <Button size="sm" variant="outline" on:click={() => (editingRule = null)}>Cancel</Button>
              </div>
            </form>
          </Dialog>
        {/if}
      </div>
      </CardUI.Content>
    </CardUI.Root>
        {:else if tab === "keys"}
    <CardUI.Root class="gap-0">
      <CardUI.Header>
        <CardUI.Title>API keys</CardUI.Title>
        <CardUI.Description>Bearer tokens for automation: <span class="font-mono">ingest</span> posts events without a DSN, <span class="font-mono">read</span>/<span class="font-mono">write</span> use the management API without a session. Optionally bound to one project and with an expiry. Secrets are shown once — only hashes are stored.</CardUI.Description>
      </CardUI.Header>
      <CardUI.Content>
      <div class="space-y-3">
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
      </CardUI.Content>
    </CardUI.Root>
        {:else if tab === "users"}
    <CardUI.Root class="gap-0">
      <CardUI.Header>
        <CardUI.Title>Users</CardUI.Title>
        <CardUI.Description>Admins bypass every check. Members only see projects granted to them directly or through a team.</CardUI.Description>
      </CardUI.Header>
      <CardUI.Content>
      <div class="space-y-4">
        {#if users.length}
          <div class="divide-y divide-border rounded-md border border-border">
            {#each users as u}
              {@const isSelf = u.username === meUsername}
              <div class="flex flex-wrap items-center justify-between gap-2 p-2 text-xs">
                <span>
                  <span class="font-medium">{u.username}</span>
                  {#if isSelf}<span class="text-muted-foreground"> (you)</span>{/if}
                  <span class="text-muted-foreground"> · {u.role}</span>
                  {#if u.email}<span class="text-muted-foreground"> · {u.email}</span>{/if}
                  {#if u.totpEnabled}
                    <span class="ml-1 rounded-md bg-green-600/15 px-1.5 py-0.5 text-[11px] font-medium text-green-600" title="Two-factor authentication enabled">2FA</span>
                  {/if}
                  {#if u.sso}
                    <span class="ml-1 rounded-md bg-blue-600/15 px-1.5 py-0.5 text-[11px] font-medium text-blue-600" title="Single sign-on linked">SSO</span>
                  {/if}
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
                  {#if u.totpEnabled}
                    <Button size="sm" variant="outline" on:click={() => resetUser2fa(u.id, u.username)}>Reset 2FA</Button>
                  {/if}
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
                <div class="space-y-1"><label class="text-xs">Email (optional, links SSO)</label><Input bind:value={newUser.email} placeholder="jane@example.com" autocomplete="off" /></div>
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
      </CardUI.Content>
    </CardUI.Root>
    <CardUI.Root class="gap-0">
      <CardUI.Header>
        <CardUI.Title>Teams</CardUI.Title>
        <CardUI.Description>Group users, then grant whole teams to projects. A member sees a project with a direct grant or any team grant.</CardUI.Description>
      </CardUI.Header>
      <CardUI.Content>
      <div class="space-y-4">
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
      </CardUI.Content>
    </CardUI.Root>
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
        {:else if tab === "audit"}
    <CardUI.Root class="gap-0">
      <CardUI.Header>
        <CardUI.Title>Audit log</CardUI.Title>
        <CardUI.Description>Authentication and management actions, newest first. Append-only — entries are never edited or swept.</CardUI.Description>
      </CardUI.Header>
      <CardUI.Content>
      <div class="space-y-3">
        <form class="flex gap-2" on:submit={(e) => { e.preventDefault(); auditOffset = 0; loadAudit(); }}>
          <Input bind:value={auditFilter} placeholder="Filter by action prefix, e.g. auth. or users." />
          <Button type="submit" size="sm" variant="outline">Filter</Button>
          <Button size="sm" variant="outline" on:click={loadAudit}>Refresh</Button>
        </form>
        {#if auditEntries.length}
          <div class="divide-y divide-border rounded-md border border-border">
            {#each auditEntries as e}
              <div class="space-y-0.5 p-2 text-xs">
                <p>
                  <span class="font-mono font-medium">{e.action}</span>
                  <span class="text-muted-foreground"> · {e.actorUsername || "(anonymous)"}{e.entityType ? ` · ${e.entityType}${e.entityId ? " " + e.entityId : ""}` : ""}</span>
                </p>
                <p class="font-mono text-[11px] text-muted-foreground">{e.createdAt}{e.ip ? ` · ${e.ip}` : ""}{e.detail && e.detail !== "{}" ? ` · ${e.detail}` : ""}</p>
              </div>
            {/each}
          </div>
          <div class="flex items-center justify-between text-xs text-muted-foreground">
            <span>{auditTotal === 0 ? "No entries" : `${auditOffset + 1}–${Math.min(auditOffset + auditPageSize, auditTotal)} of ${auditTotal}`}</span>
            <span class="flex gap-2">
              <Button size="sm" variant="outline" disabled={auditOffset === 0 || auditBusy} on:click={() => auditPage(-1)}>Newer</Button>
              <Button size="sm" variant="outline" disabled={auditOffset + auditPageSize >= auditTotal || auditBusy} on:click={() => auditPage(1)}>Older</Button>
            </span>
          </div>
        {:else}
          <p class="text-xs text-muted-foreground">{auditBusy ? "Loading…" : "No entries yet."}</p>
        {/if}
      </div>
      </CardUI.Content>
    </CardUI.Root>
        {:else if tab === "storage"}
    <CardUI.Root class="gap-0">
      <CardUI.Header>
        <CardUI.Title>Storage</CardUI.Title>
        <CardUI.Description>
          {#if !storage}
            Loading usage…
          {:else}
            Backend: <span class="font-mono">{storage.driver}</span>
            · Database size: <span class="font-mono">{formatBytes(storage.sizeBytes)}</span>
          {/if}
        </CardUI.Description>
      </CardUI.Header>
      <CardUI.Content>
      <div class="space-y-3">
        {#if storage}
          <div class="grid grid-cols-2 gap-1 text-xs sm:grid-cols-3">
            {#each storage.tables as t}
              <div class="flex justify-between gap-2 rounded bg-muted px-2 py-1">
                <span class="font-mono text-muted-foreground">{t.name}</span>
                <span class="font-mono tabular-nums">{t.rows.toLocaleString()}</span>
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
      </CardUI.Content>
    </CardUI.Root>
        {/if}
      </div>
    </div>
  {/if}
</div>
