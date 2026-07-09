<script lang="ts">
	import { onMount } from "svelte";
	import { Card, Badge, Button, Spinner } from "$ui";
	import { forge } from "$lib/goforge";
	import { api } from "$lib/api";
	import { session } from "$lib/session.svelte";
	import { EXAMPLE_COLLECTION } from "$lib/nav";

	let loading = $state(true);
	let itemCount = $state(0);
	let orgCount = $state(0);
	let recent = $state<Record<string, unknown>[]>([]);

	onMount(async () => {
		try {
			const items = await forge.list(EXAMPLE_COLLECTION, { perPage: 5, sort: "-created" });
			itemCount = items.totalItems;
			recent = items.items;
		} catch {
			/* the demo collection may have been deleted in the admin */
		}
		try {
			const orgs = await api.listOrgs();
			orgCount = orgs.totalItems;
		} catch {
			/* orgs optional */
		}
		loading = false;
	});
</script>

<h1 class="h1">Welcome back</h1>
<p class="forge-muted sub">Signed in as {session.email}</p>

{#if loading}
	<div class="loading"><Spinner /></div>
{:else}
	<div class="stats">
		<Card><div class="stat"><span class="slabel">Items</span><span class="value">{itemCount}</span></div></Card>
		<Card><div class="stat"><span class="slabel">Organizations</span><span class="value">{orgCount}</span></div></Card>
		<Card><div class="stat"><span class="slabel">Plan</span><span class="value">Free</span></div></Card>
	</div>

	<div class="grid">
		<Card title="Recent items" description="Your latest rows in the items collection.">
			{#if recent.length}
				<ul class="list">
					{#each recent as r}
						<li><span class="name">{r.name}</span><Badge variant="secondary">{String(r.status ?? "—")}</Badge></li>
					{/each}
				</ul>
			{:else}
				<p class="forge-muted">No items yet. <a href="/app/data">Create your first →</a></p>
			{/if}
			<div class="cfoot"><Button href="/app/data" variant="outline" size="sm">Open items</Button></div>
		</Card>
		<Card title="Get started">
			<ul class="checks">
				<li><a href="/app/team">Create your organization →</a></li>
				<li><a href="/app/account">Set up two-factor auth →</a></li>
				<li><a href="/_/">Manage data in the admin →</a></li>
			</ul>
		</Card>
	</div>
{/if}

<style>
	.h1 { font-size: 1.5rem; font-weight: 700; margin: 0; }
	.sub { margin: 0.25rem 0 1.5rem; }
	.loading { display: flex; justify-content: center; padding: 3rem; }
	.stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 1rem; margin-bottom: 1.5rem; }
	.stat { display: flex; flex-direction: column; gap: 0.35rem; }
	.slabel { font-size: 0.8125rem; color: hsl(var(--muted-foreground)); }
	.value { font-size: 1.75rem; font-weight: 700; }
	.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 1rem; }
	.list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.6rem; }
	.list li { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; }
	.name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.checks { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.6rem; }
	.checks a { text-decoration: underline; }
	.forge-muted a { text-decoration: underline; }
	.cfoot { margin-top: 1rem; }
</style>
