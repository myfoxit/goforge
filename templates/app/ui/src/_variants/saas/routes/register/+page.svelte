<script lang="ts">
	import { onMount } from "svelte";
	import { Button, Card, Field, Input, Alert } from "$ui";
	import { forge, type OAuthProvider } from "$lib/goforge";
	import { session } from "$lib/session.svelte";
	import { APP_NAME } from "$lib/brand";
	import { goto } from "$app/navigation";
	import { toaster } from "$ui";

	let email = $state("");
	let password = $state("");
	let confirm = $state("");
	let error = $state("");
	let loading = $state(false);
	let providers = $state<OAuthProvider[]>([]);

	onMount(async () => {
		try {
			const methods = await forge.authMethods("users");
			providers = methods.oauth2?.providers ?? [];
		} catch {
			/* best-effort */
		}
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
			await session.register(email, password);
			toaster.success("Welcome!", "Check your inbox to verify your email.");
			goto("/app");
		} catch (err) {
			error = (err as Error).message;
		} finally {
			loading = false;
		}
	}

	function oauth(p: OAuthProvider) {
		location.href = forge.oauthURL("users", p.name, "/app");
	}
</script>

<div class="auth">
	<Card title="Create your account" description={`Get started with ${APP_NAME}.`}>
		<form onsubmit={submit} class="forge-stack">
			{#if error}<Alert variant="destructive">{error}</Alert>{/if}
			<Field label="Email" for="email">
				<Input id="email" type="email" bind:value={email} placeholder="you@example.com" required />
			</Field>
			<Field label="Password" for="password" hint="At least 10 characters.">
				<Input id="password" type="password" bind:value={password} required minlength={10} />
			</Field>
			<Field label="Confirm password" for="confirm">
				<Input id="confirm" type="password" bind:value={confirm} required />
			</Field>
			<Button type="submit" loading={loading} class="full">Create account</Button>
		</form>

		{#if providers.length}
			<div class="or"><span>or sign up with</span></div>
			<div class="forge-stack providers">
				{#each providers as p}
					<Button variant="outline" class="full" onclick={() => oauth(p)}>{p.displayName}</Button>
				{/each}
			</div>
		{/if}

		<p class="sub center">Already have an account? <a href="/login">Sign in</a></p>
	</Card>
</div>

<style>
	.auth { max-width: 400px; margin: 4rem auto; padding: 0 1rem; }
	.center { text-align: center; }
	.sub { font-size: 0.875rem; color: hsl(var(--muted-foreground)); }
	.center a { text-decoration: underline; color: hsl(var(--foreground)); }
	.providers { margin-top: 0.25rem; gap: 0.5rem; }
	.or { display: flex; align-items: center; gap: 0.75rem; margin: 1.25rem 0 0.75rem; color: hsl(var(--muted-foreground)); font-size: 0.8125rem; }
	.or::before, .or::after { content: ""; flex: 1; height: 1px; background: hsl(var(--border)); }
</style>
