<script lang="ts">
  import { onMount } from "svelte";
  import { api, ApiError, type SSOConfig } from "$lib/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import TwoFASetup from "$lib/components/TwoFASetup.svelte";
  import { TriangleAlert, KeyRound, Check } from "lucide-svelte";
  import logoUrl from "$lib/assets/splat-long.png";

  export let onLoggedIn: () => void;

  let username = "";
  let password = "";
  let error = "";
  let busy = false;

  // Password step done; waiting on the second factor (or enrollment).
  let challenge: string | null = null;
  let enrollRequired = false;
  let code = "";
  let copied: Record<string, boolean> = {};
  let recoveryCodes: string[] | null = null;

  let sso: SSOConfig | null = null;

  const ssoErrors: Record<string, string> = {
    provider: "The identity provider denied the request.",
    state: "The sign-in attempt expired or was tampered with. Try again.",
    exchange: "Could not complete sign-in with the identity provider.",
    verify: "The identity provider response was invalid.",
    email: "No verified email address was provided. Check your identity provider account.",
    server: "Single sign-on failed. Contact your administrator.",
  };

  onMount(async () => {
    const params = new URLSearchParams(window.location.search);
    const ssoError = params.get("ssoError");
    if (ssoError) {
      error = ssoErrors[ssoError] ?? "Single sign-on failed.";
      window.history.replaceState({}, "", "/login");
    }
    try {
      sso = await api.ssoConfig();
    } catch {
      sso = null;
    }
  });

  async function submit(e: Event) {
    e.preventDefault();
    busy = true;
    error = "";
    try {
      const res = await api.login(username, password);
      if (res.twoFactorRequired && res.challenge) {
        challenge = res.challenge;
        enrollRequired = !!res.enrollRequired;
      } else {
        onLoggedIn();
      }
    } catch (err) {
      error = err instanceof ApiError ? "Invalid username or password." : "Login failed.";
    } finally {
      busy = false;
    }
  }

  async function submitCode(e: Event) {
    e.preventDefault();
    if (!challenge) return;
    busy = true;
    error = "";
    try {
      await api.login2fa(challenge, code);
      onLoggedIn();
    } catch (err) {
      error =
        err instanceof ApiError && err.status === 401
          ? "Invalid code. Try the current code from your authenticator app, or a recovery code."
          : err instanceof Error
            ? err.message
            : "Verification failed.";
    } finally {
      busy = false;
    }
  }

  function enrolled(codes: string[]) {
    // Enrollment during login already issued the session server-side.
    recoveryCodes = codes;
  }

  function back() {
    challenge = null;
    enrollRequired = false;
    code = "";
    error = "";
    recoveryCodes = null;
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

<div class="flex min-h-screen items-center justify-center p-4">
  <Card.Root class="w-full max-w-sm gap-0">
    <Card.Header class="pb-6">
      <img src={logoUrl} alt="Splat" class="h-10 w-auto max-w-full self-start object-contain" />
      <div>
        <Card.Description>
          {#if recoveryCodes}Save your recovery codes
          {:else if challenge && enrollRequired}Set up two-factor authentication
          {:else if challenge}Two-factor authentication
          {:else}Sign in to continue{/if}
        </Card.Description>
      </div>
    </Card.Header>
    <Card.Content>
      {#if recoveryCodes}
        <div class="space-y-4">
          <Alert.Root>
            <KeyRound />
            <Alert.Title>One-time only</Alert.Title>
            <Alert.Description>These recovery codes replace your authenticator if you lose it. Each works once — store them somewhere safe.</Alert.Description>
          </Alert.Root>
          <ul class="space-y-1">
            {#each recoveryCodes as c}
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
          <Button class="w-full" on:click={onLoggedIn}>I've saved them — continue</Button>
        </div>
      {:else if challenge && enrollRequired}
        <TwoFASetup {challenge} submitLabel="Verify & sign in" onDone={enrolled} />
        <button class="mt-3 w-full text-center text-xs text-muted-foreground hover:text-foreground" on:click={back}>Back to sign-in</button>
      {:else if challenge}
        <form on:submit={submitCode} class="space-y-4">
          <div class="space-y-2">
            <label class="text-sm font-medium" for="code">Authenticator or recovery code</label>
            <Input name="code" bind:value={code} required placeholder="123456" />
            <p class="text-xs text-muted-foreground">Signed in as {username} — enter the 6-digit code from your authenticator app, or a recovery code.</p>
          </div>
          {#if error}
            <Alert.Root variant="destructive">
              <TriangleAlert />
              <Alert.Title>Verification failed</Alert.Title>
              <Alert.Description>{error}</Alert.Description>
            </Alert.Root>
          {/if}
          <Button type="submit" disabled={busy} class="w-full">{busy ? "Verifying…" : "Verify"}</Button>
          <button type="button" class="w-full text-center text-xs text-muted-foreground hover:text-foreground" on:click={back}>Back to sign-in</button>
        </form>
      {:else}
        {#if sso?.enabled}
          <div class="space-y-4">
            <Button class="w-full" variant={sso.passwordLoginDisabled ? "default" : "outline"} on:click={() => (window.location.href = "/api/v1/sso/login")}>
              Sign in with {sso.name || "SSO"}
            </Button>
            {#if !sso.passwordLoginDisabled}
              <div class="flex items-center gap-2 text-xs text-muted-foreground">
                <span class="h-px flex-1 bg-border" /> or with password <span class="h-px flex-1 bg-border" />
              </div>
            {/if}
          </div>
        {/if}
        {#if !(sso?.enabled && sso.passwordLoginDisabled)}
        <form on:submit={submit} class="space-y-4">
          <div class="space-y-2">
            <label class="text-sm font-medium" for="username">Username</label>
            <Input name="username" bind:value={username} required placeholder="admin" />
          </div>
          <div class="space-y-2">
            <label class="text-sm font-medium" for="password">Password</label>
            <Input name="password" type="password" bind:value={password} required placeholder="••••••••" />
          </div>
          {#if error}
            <Alert.Root variant="destructive">
              <TriangleAlert />
              <Alert.Title>Sign-in failed</Alert.Title>
              <Alert.Description>{error}</Alert.Description>
            </Alert.Root>
          {/if}
          <Button type="submit" disabled={busy} class="w-full">{busy ? "Signing in…" : "Sign in"}</Button>
        </form>
        {:else if error}
          <Alert.Root variant="destructive">
            <TriangleAlert />
            <Alert.Title>Sign-in failed</Alert.Title>
            <Alert.Description>{error}</Alert.Description>
          </Alert.Root>
        {/if}
      {/if}
    </Card.Content>
  </Card.Root>
</div>
