<script lang="ts">
  import { onMount } from "svelte";
  import { api, ApiError, type InviteInfo } from "$lib/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import { Bug, TriangleAlert } from "lucide-svelte";

  export let token: string;
  export let navigate: (to: string) => void;

  let info: InviteInfo | null = null;
  let error = "";
  let username = "";
  let password = "";
  let confirm = "";
  let busy = false;

  onMount(async () => {
    try {
      info = await api.getInvite(token);
      username = info.username;
    } catch (e) {
      error = e instanceof ApiError && (e.status === 404 || e.status === 410)
        ? "This invite link is invalid or expired. Ask an admin for a new one."
        : e instanceof Error ? e.message : "Could not load invitation.";
    }
  });

  async function submit(e: Event) {
    e.preventDefault();
    if (password !== confirm) {
      error = "Passwords do not match.";
      return;
    }
    busy = true;
    error = "";
    try {
      await api.acceptInvite({ token, username: username.trim() || undefined as unknown as string, password });
      navigate("/");
      window.location.reload();
    } catch (err) {
      error = err instanceof Error ? err.message : "Could not accept invitation.";
    } finally {
      busy = false;
    }
  }
</script>

<div class="flex min-h-screen items-center justify-center p-4">
  <Card.Root class="w-full max-w-sm gap-0">
    <Card.Header class="flex-row items-center gap-2 pb-6">
      <span class="bg-primary flex size-9 items-center justify-center rounded-lg"><Bug class="text-primary-foreground size-5" /></span>
      <div><Card.Title class="text-lg">You're invited</Card.Title>
      <Card.Description>{info ? `Join Splat as ${info.email} (${info.role})` : "Accept your team invitation"}</Card.Description></div>
    </Card.Header>
    <Card.Content>
      {#if error && !info}
        <Alert.Root variant="destructive"><TriangleAlert /><Alert.Title>Invite unavailable</Alert.Title><Alert.Description>{error}</Alert.Description></Alert.Root>
        <Button class="mt-4 w-full" variant="outline" on:click={() => navigate("/login")}>Back to sign-in</Button>
      {:else if !info}
        <p class="text-sm text-muted-foreground">Loading invitation…</p>
      {:else}
        <form on:submit={submit} class="space-y-4">
          <div class="space-y-2"><label class="text-sm font-medium" for="username">Username</label><Input name="username" bind:value={username} required placeholder="jane" /></div>
          <div class="space-y-2"><label class="text-sm font-medium" for="password">Password (min 8 chars)</label><Input name="password" type="password" bind:value={password} required placeholder="••••••••" /></div>
          <div class="space-y-2"><label class="text-sm font-medium" for="confirm">Confirm password</label><Input name="confirm" type="password" bind:value={confirm} required placeholder="••••••••" /></div>
          {#if error}
            <Alert.Root variant="destructive"><TriangleAlert /><Alert.Title>Could not accept</Alert.Title><Alert.Description>{error}</Alert.Description></Alert.Root>
          {/if}
          <Button type="submit" disabled={busy} class="w-full">{busy ? "Joining…" : "Accept invite & sign in"}</Button>
        </form>
      {/if}
    </Card.Content>
  </Card.Root>
</div>
