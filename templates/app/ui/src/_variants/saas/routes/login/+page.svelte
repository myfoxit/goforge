<script lang="ts">
	import { onMount } from "svelte";
	import { Button, Card, Field, Input, Alert } from "$ui";
	import { forge, type ForgeError, type OAuthProvider } from "$lib/goforge";
	import { session } from "$lib/session.svelte";
	import { APP_NAME } from "$lib/brand";
	import { goto } from "$app/navigation";

	let email = $state("");
	let password = $state("");
	let error = $state("");
	let loading = $state(false);

	// Second factor, set once a password login answers with mfaRequired.
	let mfaToken = $state("");
	let code = $state("");

	let providers = $state<OAuthProvider[]>([]);

	onMount(async () => {
		try {
			const methods = await forge.authMethods("users");
			providers = methods.oauth2?.providers ?? [];
		} catch {
			/* auth-methods is best-effort */
		}
	});

	async function submit(e: Event) {
		e.preventDefault();
		error = "";
		loading = true;
		try {
			await session.login(email, password);
			goto("/app");
		} catch (err) {
			const fe = err as ForgeError;
			if (fe.data?.mfaToken) mfaToken = fe.data.mfaToken;
			else error = fe.message;
		} finally {
			loading = false;
		}
	}

	async function submitMFA(e: Event) {
		e.preventDefault();
		error = "";
		loading = true;
		try {
			await session.completeMFA(mfaToken, code);
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
	<Card
		title={mfaToken ? "Two-factor code" : "Sign in"}
		description={mfaToken ? "Enter the 6-digit code from your authenticator app." : `Welcome back to ${APP_NAME}.`}
	>
		{#if mfaToken}
			<form onsubmit={submitMFA} class="forge-stack">
				{#if error}<Alert variant="destructive">{error}</Alert>{/if}
				<Field label="Authentication code" for="code">
					<Input id="code" inputmode="numeric" autocomplete="one-time-code" bind:value={code} placeholder="123456" required />
				</Field>
				<Button type="submit" loading={loading} class="full">Verify</Button>
			</form>
		{:else}
			<form onsubmit={submit} class="forge-stack">
				{#if error}<Alert variant="destructive">{error}</Alert>{/if}
				<Field label="Email" for="email">
					<Input id="email" type="email" bind:value={email} placeholder="you@example.com" required />
				</Field>
				<Field label="Password" for="password">
					<Input id="password" type="password" bind:value={password} required />
				</Field>
				<div class="forgot"><a class="sub" href="/forgot-password">Forgot password?</a></div>
				<Button type="submit" loading={loading} class="full">Sign in</Button>
			</form>

			{#if providers.length}
				<div class="or"><span>or continue with</span></div>
				<div class="forge-stack providers">
					{#each providers as p}
						<Button variant="outline" class="full" onclick={() => oauth(p)}>{p.displayName}</Button>
					{/each}
				</div>
			{/if}

			<p class="sub center">No account? <a href="/register">Create one</a></p>
		{/if}
	</Card>
</div>

<style>
	.auth { max-width: 400px; margin: 4rem auto; padding: 0 1rem; }
	.center { text-align: center; }
	.sub { font-size: 0.875rem; color: hsl(var(--muted-foreground)); }
	.sub a, .center a { text-decoration: underline; color: hsl(var(--foreground)); }
	.forgot { display: flex; justify-content: flex-end; margin-top: -0.35rem; }
	.providers { margin-top: 0.25rem; gap: 0.5rem; }
	.or { display: flex; align-items: center; gap: 0.75rem; margin: 1.25rem 0 0.75rem; color: hsl(var(--muted-foreground)); font-size: 0.8125rem; }
	.or::before, .or::after { content: ""; flex: 1; height: 1px; background: hsl(var(--border)); }
</style>
