<script lang="ts">
	import { onMount } from "svelte";
	import { Card, Alert, Button, Spinner } from "$ui";
	import { forge } from "$lib/goforge";

	let phase = $state<"working" | "ok" | "error">("working");
	let message = $state("");

	onMount(async () => {
		const token = new URLSearchParams(location.search).get("token") ?? "";
		if (!token) {
			phase = "error";
			message = "This verification link is missing its token.";
			return;
		}
		try {
			await forge.confirmVerification("users", token);
			phase = "ok";
		} catch (err) {
			phase = "error";
			message = (err as Error).message || "This link is invalid or has expired.";
		}
	});
</script>

<div class="auth">
	<Card title="Email verification">
		{#if phase === "working"}
			<div class="center"><Spinner /><p class="sub">Confirming your email…</p></div>
		{:else if phase === "ok"}
			<Alert variant="success">Your email is verified. You're all set.</Alert>
			<div class="center"><Button href="/app">Go to dashboard</Button></div>
		{:else}
			<Alert variant="destructive">{message}</Alert>
			<div class="center"><Button href="/login" variant="outline">Back to sign in</Button></div>
		{/if}
	</Card>
</div>

<style>
	.auth { max-width: 400px; margin: 4rem auto; padding: 0 1rem; }
	.center { text-align: center; margin-top: 1rem; display: flex; flex-direction: column; align-items: center; gap: 0.75rem; }
	.sub { font-size: 0.875rem; color: hsl(var(--muted-foreground)); }
</style>
