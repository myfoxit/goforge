<script lang="ts">
	import { Button, Badge, Card, ThemeToggle } from "$ui";
	import { APP_NAME } from "$lib/brand";
	import { session } from "$lib/session.svelte";

	const features = [
		{ title: "Auth & SSO", body: "Email/password, OAuth providers, MFA and password reset — all wired up." },
		{ title: "Teams & orgs", body: "Multi-tenant organizations with member roles and email invites." },
		{ title: "Your data", body: "Collections with access rules, realtime updates and a typed client." },
	];
</script>

<header class="topbar">
	<span class="brand">⚒ {APP_NAME}</span>
	<nav class="forge-row">
		<ThemeToggle />
		{#if session.isAuthed}
			<Button href="/app" size="sm">Open app</Button>
		{:else}
			<a class="signin" href="/login">Sign in</a>
			<Button href="/register" size="sm">Get started</Button>
		{/if}
	</nav>
</header>

<main class="wrap">
	<section class="hero">
		<Badge variant="secondary">Built with GoForge</Badge>
		<h1>The {APP_NAME} platform</h1>
		<p class="lead">
			A complete SaaS starter — authentication, teams, billing and an admin console,
			compiled into a single Go binary.
		</p>
		<div class="cta">
			{#if session.isAuthed}
				<Button href="/app" size="lg">Go to dashboard</Button>
			{:else}
				<Button href="/register" size="lg">Create your account</Button>
				<Button href="/login" size="lg" variant="outline">Sign in</Button>
			{/if}
		</div>
	</section>

	<section class="grid">
		{#each features as f}
			<Card title={f.title}>
				<p class="forge-muted">{f.body}</p>
			</Card>
		{/each}
	</section>
</main>

<footer class="foot forge-muted">
	Built with <a href="https://github.com/myfoxit/goforge">GoForge</a> · <a href="/_/">Admin</a>
</footer>

<style>
	.topbar {
		display: flex; align-items: center; justify-content: space-between;
		padding: 0.85rem 1.5rem; border-bottom: 1px solid hsl(var(--border));
	}
	.brand { font-weight: 700; font-size: 1.05rem; }
	.signin { color: hsl(var(--muted-foreground)); font-size: 0.875rem; }
	.signin:hover { color: hsl(var(--foreground)); }
	.wrap { max-width: 960px; margin: 0 auto; padding: 0 1.5rem; }
	.hero { text-align: center; padding: 5rem 0 3.5rem; }
	.hero h1 { font-size: clamp(2rem, 5vw, 3rem); font-weight: 800; margin: 1rem 0 0.75rem; letter-spacing: -0.02em; }
	.lead { max-width: 34rem; margin: 0 auto; color: hsl(var(--muted-foreground)); font-size: 1.075rem; line-height: 1.6; }
	.cta { display: flex; gap: 0.75rem; justify-content: center; flex-wrap: wrap; margin-top: 1.75rem; }
	.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 1rem; padding-bottom: 4rem; }
	.foot { text-align: center; padding: 2rem 1.5rem; font-size: 0.8125rem; border-top: 1px solid hsl(var(--border)); }
	.foot a { text-decoration: underline; }
</style>
