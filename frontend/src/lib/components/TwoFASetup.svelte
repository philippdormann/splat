<script lang="ts">
  import { onMount } from "svelte";
  import { api, ApiError, type TwoFABegin } from "$lib/api";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import { TriangleAlert, Copy, Check } from "lucide-svelte";

  // challenge authorizes first-time enrollment during login (no session
  // yet). Without it the session cookie authorizes setup/rotation.
  export let challenge: string | null = null;
  export let submitLabel = "Verify & enable";
  export let onDone: (codes: string[]) => void;

  let begun: TwoFABegin | null = null;
  let loadError = "";
  let code = "";
  let error = "";
  let busy = false;
  let copied = false;

  onMount(async () => {
    try {
      begun = await api.twofaBegin(challenge ?? undefined);
    } catch (err) {
      loadError = err instanceof ApiError ? err.message : "Could not start setup.";
    }
  });

  async function confirm(e: Event) {
    e.preventDefault();
    busy = true;
    error = "";
    try {
      const res = await api.twofaConfirm(code, challenge ?? undefined);
      onDone(res.recoveryCodes ?? []);
    } catch (err) {
      error = err instanceof ApiError ? err.message : "Verification failed.";
    } finally {
      busy = false;
    }
  }

  async function copySecret() {
    if (!begun) return;
    try {
      await navigator.clipboard.writeText(begun.secret);
      copied = true;
      setTimeout(() => (copied = false), 2000);
    } catch {
      // Clipboard unavailable (non-secure context): the secret is still visible below.
    }
  }
</script>

{#if loadError}
  <Alert.Root variant="destructive">
    <TriangleAlert />
    <Alert.Title>Setup failed</Alert.Title>
    <Alert.Description>{loadError}</Alert.Description>
  </Alert.Root>
{:else if !begun}
  <p class="text-sm text-muted-foreground">Preparing authenticator setup…</p>
{:else}
  <div class="space-y-4">
    <ol class="list-decimal space-y-1 pl-5 text-sm text-muted-foreground">
      <li>Open your authenticator app (Google Authenticator, 1Password, …).</li>
      <li>Scan the code below or enter the secret manually.</li>
      <li>Enter the 6-digit code it shows to verify.</li>
    </ol>
    <div class="flex justify-center rounded-lg border border-border bg-white p-3">
      <img src={begun.qrDataUri} alt="Authenticator QR code" class="size-48" />
    </div>
    <div class="flex items-center gap-2">
      <code class="min-w-0 flex-1 truncate rounded-md bg-muted px-2 py-1.5 font-mono text-xs">{begun.secret}</code>
      <Button size="sm" variant="outline" on:click={copySecret}>
        {#if copied}<Check class="size-4" />{:else}<Copy class="size-4" />{/if}
        {copied ? "Copied" : "Copy"}
      </Button>
    </div>
    <form on:submit={confirm} class="space-y-3">
      <div class="space-y-2">
        <label class="text-sm font-medium" for="totp-code">Authenticator code</label>
        <Input
          name="totp-code"
          bind:value={code}
          required
          placeholder="123456"
        />
      </div>
      {#if error}
        <Alert.Root variant="destructive">
          <TriangleAlert />
          <Alert.Title>Verification failed</Alert.Title>
          <Alert.Description>{error}</Alert.Description>
        </Alert.Root>
      {/if}
      <Button type="submit" disabled={busy} class="w-full">{busy ? "Verifying…" : submitLabel}</Button>
    </form>
  </div>
{/if}
