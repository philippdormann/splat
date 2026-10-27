<script lang="ts">
  import { api, ApiError } from "$lib/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import { Bug, TriangleAlert } from "lucide-svelte";

  export let onLoggedIn: () => void;

  let username = "";
  let password = "";
  let error = "";
  let busy = false;

  async function submit(e: Event) {
    e.preventDefault();
    busy = true;
    error = "";
    try {
      await api.login(username, password);
      onLoggedIn();
    } catch (err) {
      error = err instanceof ApiError ? "Invalid username or password." : "Login failed.";
    } finally {
      busy = false;
    }
  }
</script>

<div class="flex min-h-screen items-center justify-center p-4">
  <Card.Root class="w-full max-w-sm gap-0">
    <Card.Header class="flex-row items-center gap-2 pb-6">
      <span class="bg-primary flex size-9 items-center justify-center rounded-lg">
        <Bug class="text-primary-foreground size-5" />
      </span>
      <div>
        <Card.Title class="text-lg">Splat</Card.Title>
        <Card.Description>Sign in to continue</Card.Description>
      </div>
    </Card.Header>
    <Card.Content>
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
    </Card.Content>
  </Card.Root>
</div>
