<script lang="ts">
  import { api, ApiError } from "$lib/api";
  import Card from "$lib/components/card.svelte";
  import Input from "$lib/components/input.svelte";
  import Button from "$lib/components/button.svelte";
  import { Bug } from "lucide-svelte";

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
  <Card cls="w-full max-w-sm p-6">
    <div class="mb-6 flex items-center gap-2 text-lg font-semibold">
      <Bug class="h-5 w-5" /> Errgotrack
    </div>
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
        <p class="text-sm text-destructive">{error}</p>
      {/if}
      <Button type="submit" disabled={busy} class="w-full">{busy ? "Signing in…" : "Sign in"}</Button>
    </form>
  </Card>
</div>
