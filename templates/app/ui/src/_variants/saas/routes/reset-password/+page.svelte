<script lang="ts">
	import { onMount } from "svelte";
	import { Button, Card, Field, Input, Alert } from "$ui";
	import { forge } from "$lib/goforge";
	import { goto } from "$app/navigation";

	let token = $state("");
	let password = $state("");
	let confirm = $state("");
	let error = $state("");
	let done = $state(false);
	let loading = $state(false);

	onMount(() => {
		token = new URLSearchParams(location.search).get("token") ?? "";
	});

	async function submit(e: Event) {
		e.preventDefault();
		error = "";
		if (password !== confirm) {
			error = "Passwords do not match.";
			return;
		}
		loading = true;
		try {
			await forge.confirmPasswordReset("users", token, password);
			done = true;
			setTimeout(() => goto("/login"), 1500);
		} catch (err) {
			error = (err as Error).message;
		} finally {
			loading = false;
		}
	}
</script>

<div class="auth">
	<Card title="Choose a new password" description="Set the new password for your account.">
		{#if !token}
			<Alert variant="destructive">This reset link is missing its token or has expired.</Alert>
			<p class="sub center"><a href="/forgot-password">Request a new link</a></p>
		{:else if done}
			<Alert variant="success">Password updated. Redirecting to sign in…</Alert>
		{:else}
			<form onsubmit={submit} class="forge-stack">
				{#if error}<Alert variant="destructive">{error}</Alert>{/if}
				<Field label="New password" for="password" hint="At least 10 characters.">
					<Input id="password" type="password" bind:value={password} required minlength={10} />
				</Field>
				<Field label="Confirm password" for="confirm">
					<Input id="confirm" type="password" bind:value={confirm} required />
				</Field>
				<Button type="submit" loading={loading} class="full">Update password</Button>
			</form>
		{/if}
	</Card>
</div>

<style>
	.auth { max-width: 400px; margin: 4rem auto; padding: 0 1rem; }
	.center { text-align: center; margin-top: 1rem; }
	.sub { font-size: 0.875rem; color: hsl(var(--muted-foreground)); }
	.center a { text-decoration: underline; color: hsl(var(--foreground)); }
</style>
