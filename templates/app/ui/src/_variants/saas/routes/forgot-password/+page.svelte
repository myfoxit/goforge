<script lang="ts">
	import { Button, Card, Field, Input, Alert } from "$ui";
	import { forge } from "$lib/goforge";

	let email = $state("");
	let error = $state("");
	let sent = $state(false);
	let loading = $state(false);

	async function submit(e: Event) {
		e.preventDefault();
		error = "";
		loading = true;
		try {
			await forge.requestPasswordReset("users", email);
			sent = true;
		} catch (err) {
			// Don't leak whether the address exists — treat as sent.
			sent = true;
		} finally {
			loading = false;
		}
	}
</script>

<div class="auth">
	<Card title="Reset your password" description="We'll email you a link to choose a new password.">
		{#if sent}
			<Alert variant="success">
				If an account exists for <strong>{email}</strong>, a reset link is on its way.
			</Alert>
			<p class="sub center"><a href="/login">Back to sign in</a></p>
		{:else}
			<form onsubmit={submit} class="forge-stack">
				{#if error}<Alert variant="destructive">{error}</Alert>{/if}
				<Field label="Email" for="email">
					<Input id="email" type="email" bind:value={email} placeholder="you@example.com" required />
				</Field>
				<Button type="submit" loading={loading} class="full">Send reset link</Button>
				<p class="sub center"><a href="/login">Back to sign in</a></p>
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
