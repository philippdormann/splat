<script lang="ts">
  import { onMount } from "svelte";
  import { api, type SettingsData } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";

  let data: SettingsData | null = null;
  let error = "";
  let notice = "";
  let telegram = { botToken: "", chatId: "", enabled: false };
  let webhooks: Record<string, { url: string; enabled: boolean }> = {};
  let traceURL = "";

  const channels = ["slack", "discord", "teams", "sheets"];

  onMount(async () => {
    try {
      data = await api.settings();
      telegram = { botToken: "", chatId: data.telegram.chatId, enabled: data.telegram.enabled };
      for (const c of channels) webhooks[c] = { url: "", enabled: data.webhooks[c]?.enabled ?? false };
      traceURL = data.traceURLTemplate;
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load settings.";
    }
  });

  async function saveTelegram(e: Event) {
    e.preventDefault();
    data = await api.saveTelegram(telegram);
    notice = "Telegram settings saved.";
  }

  async function saveWebhook(e: Event, channel: string) {
    e.preventDefault();
    data = await api.saveWebhook(channel, webhooks[channel]);
    notice = `${channel} settings saved.`;
  }

  async function saveTracing(e: Event) {
    e.preventDefault();
    data = await api.saveTracing(traceURL);
    notice = "Tracing settings saved.";
  }
</script>

<div class="mx-auto max-w-2xl space-y-4">
  <h2 class="text-2xl font-bold">Settings</h2>
  {#if error}<p class="text-sm text-destructive">{error}</p>{/if}
  {#if notice}<p class="text-sm text-muted-foreground">{notice}</p>{/if}
  {#if !data}
    <p class="text-sm text-muted-foreground">Loading…</p>
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
          <Button size="sm" variant="outline" on:click={async () => { const r = await api.testTelegram(); notice = r.message; }}>Send test</Button>
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
            <Button size="sm" variant="outline" on:click={async () => { const r = await api.testWebhook(c); notice = r.message; }}>Send test</Button>
          </div>
        </form>
      </Card>
    {/each}

    <Card cls="p-4">
      <form on:submit={saveTracing} class="space-y-3">
        <p class="text-sm font-medium">Trace correlation</p>
        <p class="text-xs text-muted-foreground">URL template with &#123;trace_id&#125; placeholder, e.g. https://tempo.example.com/explore?traceID=&#123;trace_id&#125;</p>
        <Input bind:value={traceURL} placeholder="https://…/&#123;trace_id&#125;" />
        <Button type="submit" size="sm">Save</Button>
      </form>
    </Card>
  {/if}
</div>
