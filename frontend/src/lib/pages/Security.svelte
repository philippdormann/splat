<script lang="ts">
  import { onMount } from "svelte";
  import { api, type TwoFAStatus } from "$lib/api";
  import * as CardUI from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import TwoFASetup from "$lib/components/TwoFASetup.svelte";
  import { toast } from "svelte-sonner";
  import { TriangleAlert, ShieldCheck, ShieldOff, KeyRound, Check } from "lucide-svelte";

  let status: TwoFAStatus | null = null;
  let error = "";
  let settingUp = false;
  let freshCodes: string[] | null = null;
  let password = "";
  let busy = "";
  let copied: Record<string, boolean> = {};

  async function load() {
    try {
      status = await api.twofaStatus();
    } catch (e) {
      error = e instanceof Error ? e.message : "Could not load two-factor status.";
    }
  }

  onMount(load);

  function enrolled(codes: string[]) {
    freshCodes = codes;
    settingUp = false;
    load();
    toast.success("Two-factor authentication enabled.");
  }

  async function disable(e: Event) {
    e.preventDefault();
    busy = "disable";
    try {
      await api.twofaDisable(password);
      password = "";
      await load();
      toast.success("Two-factor authentication disabled.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not disable 2FA.");
    } finally {
      busy = "";
    }
  }

  async function regenerate(e: Event) {
    e.preventDefault();
    busy = "regen";
    try {
      const res = await api.twofaRegenCodes(password);
      password = "";
      freshCodes = res.recoveryCodes ?? [];
      await load();
      toast.success("New recovery codes issued — the old ones no longer work.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not regenerate codes.");
    } finally {
      busy = "";
    }
  }

  async function copyCode(c: string) {
    try {
      await navigator.clipboard.writeText(c);
      copied = { ...copied, [c]: true };
      setTimeout(() => (copied = { ...copied, [c]: false }), 2000);
    } catch {
      // Clipboard unavailable: codes remain visible below.
    }
  }
</script>

<div class="mx-auto w-full max-w-2xl space-y-4">
  <div>
    <h1 class="text-lg font-semibold">Security</h1>
    <p class="text-sm text-muted-foreground">Two-factor authentication for your account. API keys are separate credentials and are unaffected.</p>
  </div>

  {#if error}
    <Alert.Root variant="destructive">
      <TriangleAlert />
      <Alert.Title>Could not load</Alert.Title>
      <Alert.Description>{error}</Alert.Description>
    </Alert.Root>
  {:else if !status}
    <p class="text-sm text-muted-foreground">Loading…</p>
  {:else}
    <CardUI.Root class="gap-0">
      <CardUI.Header>
        <CardUI.Title class="flex items-center gap-2">
          {#if status.enabled}<ShieldCheck class="size-4 text-green-600" />{:else}<ShieldOff class="size-4 text-muted-foreground" />{/if}
          Two-factor authentication
        </CardUI.Title>
        <CardUI.Description>
          {#if status.enabled}
            Enabled · {status.recoveryCodesRemaining} unused recovery code{status.recoveryCodesRemaining === 1 ? "" : "s"}
          {:else if status.required}
            Required on this instance — set it up to keep signing in without interruption.
          {:else}
            Not enabled. An authenticator app plus recovery codes.
          {/if}
        </CardUI.Description>
      </CardUI.Header>
      <CardUI.Content>
        {#if !status.enabled && !settingUp}
          <Button size="sm" on:click={() => (settingUp = true)}>Enable two-factor authentication</Button>
        {:else if !status.enabled && settingUp}
          <TwoFASetup onDone={enrolled} />
          <Button size="sm" variant="outline" class="mt-3" on:click={() => (settingUp = false)}>Cancel</Button>
        {:else}
          <div class="space-y-4">
            <form on:submit={regenerate} class="flex flex-wrap items-end gap-2">
              <div class="min-w-48 flex-1 space-y-1">
                <label class="text-xs" for="regen-pw">Confirm password to issue fresh recovery codes</label>
                <Input id="regen-pw" type="password" bind:value={password} required placeholder="••••••••" />
              </div>
              <Button type="submit" size="sm" variant="outline" disabled={busy === "regen"}>{busy === "regen" ? "Working…" : "New recovery codes"}</Button>
            </form>
            {#if status.required}
              <p class="text-xs text-muted-foreground">Two-factor authentication is required on this instance and cannot be disabled. Lost your device? Ask an admin to reset it, or use a recovery code at sign-in.</p>
            {:else}
              <form on:submit={disable} class="flex flex-wrap items-end gap-2 border-t border-border pt-4">
                <div class="min-w-48 flex-1 space-y-1">
                  <label class="text-xs" for="disable-pw">Confirm password to disable</label>
                  <Input id="disable-pw" type="password" bind:value={password} required placeholder="••••••••" />
                </div>
                <Button type="submit" size="sm" variant="outline" disabled={busy === "disable"}>{busy === "disable" ? "Working…" : "Disable 2FA"}</Button>
              </form>
            {/if}
          </div>
        {/if}
      </CardUI.Content>
    </CardUI.Root>

    {#if freshCodes}
      <CardUI.Root class="gap-0">
        <CardUI.Header>
          <CardUI.Title class="flex items-center gap-2"><KeyRound class="size-4" /> Recovery codes</CardUI.Title>
          <Alert.Root class="mt-2">
            <Alert.Description>Shown once — store them somewhere safe. Each code works a single time.</Alert.Description>
          </Alert.Root>
        </CardUI.Header>
        <CardUI.Content>
          <ul class="grid grid-cols-1 gap-1 sm:grid-cols-2">
            {#each freshCodes as c}
              <li>
                <button
                  class="flex w-full items-center justify-between gap-2 rounded-md bg-muted px-2 py-1.5 font-mono text-xs hover:bg-muted/70"
                  title="Copy"
                  on:click={() => copyCode(c)}
                >
                  <span>{c}</span>
                  {#if copied[c]}<Check class="size-4" />{:else}<span class="text-muted-foreground">copy</span>{/if}
                </button>
              </li>
            {/each}
          </ul>
          <Button size="sm" variant="outline" class="mt-3" on:click={() => (freshCodes = null)}>Done</Button>
        </CardUI.Content>
      </CardUI.Root>
    {/if}
  {/if}
</div>
