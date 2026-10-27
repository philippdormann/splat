<script lang="ts">
  import { onMount } from "svelte";
  import { api, type SettingsData, type StorageUsage, type AlertRule, type AlertRuleInput, type APIKey, type GenericWebhook } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import { toast } from "svelte-sonner";
  import ConfirmDialog from "$lib/components/ConfirmDialog.svelte";

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

  const channels = ["slack", "discord", "teams", "sheets"];
  const allChannels = ["telegram", "slack", "discord", "teams", "sheets", "email", "generic"];
  const allScopes = ["ingest", "read", "write"];

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
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load settings.";
    }
  });

  async function saveTelegram(e: Event) {
    e.preventDefault();
    data = await api.saveTelegram(telegram);
    toast.success("Telegram settings saved.");
  }

  async function saveWebhook(e: Event, channel: string) {
    e.preventDefault();
    data = await api.saveWebhook(channel, webhooks[channel]);
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
      <div class="h-40 animate-pulse rounded-lg bg-muted" />
      <div class="h-40 animate-pulse rounded-lg bg-muted" />
    </div>
  {:else}
    <Card cls="p-4">
      <form on:submit={saveTelegram} class="space-y-3">
        <p class="text-sm font-medium">Telegram {#if data.telegram.enabled}<span class="text-green-600">(enabled)</span>{/if}</p>
        <p class="font-mono text-xs text-muted-foreground">Token: {data.botTokenMasked || "(not set)"}</p>
        <div class="space-y-1"><label class="text-xs">Bot token</label><Input type="password" bind:value={telegram.botToken} placeholder="leave blank to keep" /></div>
        <div class="space-y-1"><label class="text-xs">Chat ID</label><Input bind:value={telegram.chatId} /></div>
        <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={telegram.enabled} /> Enabled</label>
        <div class="flex gap-2">
          <Button type="submit" size="sm">Save</Button>
          <Button size="sm" variant="outline" on:click={async () => { const r = await api.testTelegram(); (r.ok ? toast.success(r.message) : toast.error(r.message)); }}>Send test</Button>
        </div>
      </form>
    </Card>

    {#each channels as c}
      <Card cls="p-4">
        <form on:submit={(e) => saveWebhook(e, c)} class="space-y-3">
          <p class="text-sm font-medium capitalize">{c} {#if data.webhooks[c]?.enabled}<span class="text-green-600">(enabled)</span>{/if}</p>
          <p class="font-mono text-xs text-muted-foreground">URL: {data.webhooks[c]?.urlMasked || "(not set)"}</p>
          <div class="space-y-1"><label class="text-xs">Webhook URL</label><Input bind:value={webhooks[c].url} placeholder="leave blank to keep" /></div>
          <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={webhooks[c].enabled} /> Enabled</label>
          <div class="flex gap-2">
            <Button type="submit" size="sm">Save</Button>
            <Button size="sm" variant="outline" on:click={async () => { const r = await api.testWebhook(c); (r.ok ? toast.success(r.message) : toast.error(r.message)); }}>Send test</Button>
          </div>
        </form>
      </Card>
    {/each}

    <Card cls="p-4">
      <div class="space-y-3">
        <div class="flex items-center justify-between">
          <p class="text-sm font-medium">Generic webhooks</p>
          <Button size="sm" variant="outline" on:click={newHook}>New webhook</Button>
        </div>
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
        {/if}
      </div>
    </Card>

    <Card cls="p-4">
      <form on:submit={saveTracing} class="space-y-3">
        <p class="text-sm font-medium">Trace correlation</p>
        <p class="text-xs text-muted-foreground">URL template with &#123;trace_id&#125; placeholder, e.g. https://tempo.example.com/explore?traceID=&#123;trace_id&#125;</p>
        <Input bind:value={traceURL} placeholder="https://…/&#123;trace_id&#125;" />
        <Button type="submit" size="sm">Save</Button>
      </form>
    </Card>

    <Card cls="p-4">
      <form on:submit={saveSMTP} class="space-y-3">
        <p class="text-sm font-medium">Email (SMTP) {#if data.smtp.enabled}<span class="text-green-600">(enabled)</span>{/if}</p>
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
    </Card>

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
          <form on:submit={saveRule} class="space-y-3 rounded-md border border-border p-3">
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
        {/if}
      </div>
    </Card>

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
        <form on:submit={createKey} class="space-y-3 rounded-md border border-border p-3">
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
          <Button type="submit" size="sm">Create key</Button>
        </form>
      </div>
    </Card>

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
