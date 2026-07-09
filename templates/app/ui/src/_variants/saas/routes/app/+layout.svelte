<script lang="ts">
	import { onMount } from "svelte";
	import { page } from "$app/stores";
	import { goto } from "$app/navigation";
	import { Avatar, DropdownMenu, ThemeToggle, toaster } from "$ui";
	import { session } from "$lib/session.svelte";
	import { api } from "$lib/api";
	import { nav } from "$lib/nav";
	import { APP_NAME } from "$lib/brand";
	import { initials } from "$lib/format";

	let { children } = $props();
	let ready = $state(false);
	let mobileOpen = $state(false);

	onMount(async () => {
		if (!session.isAuthed) {
			goto("/login");
			return;
		}
		await session.refresh(); // pick up role/profile changes
		if (!session.isAuthed) {
			goto("/login");
			return;
		}
		ready = true;
		await maybeAcceptInvite();
	});

	// Org invites link back with ?inviteToken=... — redeem it once signed in.
	async function maybeAcceptInvite() {
		const url = new URL(location.href);
		const token = url.searchParams.get("inviteToken");
		if (!token) return;
		try {
			const org = await api.acceptInvite(token);
			toaster.success("Joined organization", org.name);
		} catch (e) {
			toaster.error("Invite failed", (e as Error).message);
		} finally {
			url.searchParams.delete("inviteToken");
			history.replaceState(null, "", url.pathname + url.search);
		}
	}

	function isActive(href: string, exact?: boolean): boolean {
		const path = $page.url.pathname;
		return exact ? path === href : path === href || path.startsWith(href + "/");
	}

	function signOut() {
		session.logout();
		goto("/login");
	}

	const items = $derived(nav.filter((i) => !i.admin || session.isAdmin));
</script>

{#if ready}
	<div class="shell">
		<aside class="sidebar" class:open={mobileOpen}>
			<a href="/app" class="brand">⚒ {APP_NAME}</a>
			<nav class="menu">
				{#each items as item}
					<a
						href={item.href}
						class="item"
						class:active={isActive(item.href, item.exact)}
						onclick={() => (mobileOpen = false)}
					>
						<span class="ico" aria-hidden="true">{item.icon}</span>{item.label}
					</a>
				{/each}
			</nav>
			<a class="admin-link forge-muted" href="/_/">Admin console →</a>
		</aside>

		{#if mobileOpen}
			<button class="scrim" aria-label="Close menu" onclick={() => (mobileOpen = false)}></button>
		{/if}

		<div class="main">
			<header class="topbar">
				<button class="burger" aria-label="Menu" onclick={() => (mobileOpen = !mobileOpen)}>☰</button>
				<div class="spacer"></div>
				<ThemeToggle />
				<DropdownMenu align="end">
					{#snippet trigger()}
						<span class="acct-trigger"><Avatar fallback={initials(session.email)} size={30} /></span>
					{/snippet}
					{#snippet children(close)}
						<div class="acct-head">{session.email}</div>
						<div class="dropdown-separator"></div>
						<a class="dropdown-item" href="/app/account" onclick={close}>Account</a>
						<a class="dropdown-item" href="/app/billing" onclick={close}>Billing</a>
						<div class="dropdown-separator"></div>
						<button class="dropdown-item" onclick={() => { close(); signOut(); }}>Sign out</button>
					{/snippet}
				</DropdownMenu>
			</header>
			<main class="content">
				{@render children()}
			</main>
		</div>
	</div>
{/if}

<style>
	.shell { display: flex; min-height: 100vh; }
	.sidebar {
		width: 240px; flex-shrink: 0; border-right: 1px solid hsl(var(--border));
		background: hsl(var(--card)); display: flex; flex-direction: column;
		padding: 1rem 0.75rem; position: sticky; top: 0; height: 100vh;
	}
	.brand { font-weight: 700; font-size: 1.05rem; padding: 0.35rem 0.6rem 1rem; }
	.menu { display: flex; flex-direction: column; gap: 0.15rem; flex: 1; }
	.item {
		display: flex; align-items: center; gap: 0.65rem; padding: 0.5rem 0.6rem;
		border-radius: calc(var(--radius) - 2px); font-size: 0.9rem;
		color: hsl(var(--muted-foreground));
	}
	.item:hover { background: hsl(var(--accent)); color: hsl(var(--accent-foreground)); }
	.item.active { background: hsl(var(--accent)); color: hsl(var(--foreground)); font-weight: 500; }
	.ico { width: 1.1rem; text-align: center; font-size: 0.95rem; }
	.admin-link { padding: 0.5rem 0.6rem; font-size: 0.8125rem; }
	.admin-link:hover { color: hsl(var(--foreground)); }

	.main { flex: 1; display: flex; flex-direction: column; min-width: 0; }
	.topbar {
		display: flex; align-items: center; gap: 0.75rem; height: 56px;
		padding: 0 1.25rem; border-bottom: 1px solid hsl(var(--border));
		position: sticky; top: 0; background: hsl(var(--background) / 0.85);
		backdrop-filter: blur(6px); z-index: 20;
	}
	.spacer { flex: 1; }
	.burger { display: none; background: transparent; border: 0; font-size: 1.1rem; cursor: pointer; color: inherit; }
	.acct-trigger { display: inline-flex; }
	.acct-head { padding: 0.5rem 0.6rem 0.4rem; font-size: 0.8125rem; color: hsl(var(--muted-foreground)); }
	.content { padding: 1.75rem; max-width: 1080px; width: 100%; }
	.scrim { display: none; }

	@media (max-width: 820px) {
		.sidebar {
			position: fixed; z-index: 40; left: 0; top: 0; transform: translateX(-100%);
			transition: transform 0.2s ease;
		}
		.sidebar.open { transform: translateX(0); }
		.burger { display: inline-block; }
		.scrim { display: block; position: fixed; inset: 0; z-index: 35; background: hsl(0 0% 0% / 0.4); border: 0; }
	}
</style>
