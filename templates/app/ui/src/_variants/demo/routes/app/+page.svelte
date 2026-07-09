<script lang="ts">
	import { onMount } from "svelte";
	import { Button, Card, Input, Table, Badge, Spinner } from "$ui";
	import { forge } from "$lib/goforge";
	import { goto } from "$app/navigation";

	let notes = $state<Record<string, unknown>[]>([]);
	let text = $state("");
	let loading = $state(true);
	let missing = $state(false);
	let unsub: (() => void) | null = null;

	onMount(() => {
		if (!forge.isAuthenticated) { goto("/login"); return; }
		load();
		return () => unsub?.();
	});

	async function load() {
		loading = true;
		try {
			const res = await forge.list("notes", { sort: "-created", perPage: 50 });
			notes = res.items;
			unsub = forge.realtime(["notes"], () => load());
		} catch (e) {
			if ((e as { status?: number }).status === 404) missing = true;
		} finally {
			loading = false;
		}
	}

	async function add() {
		if (!text.trim()) return;
		await forge.create("notes", { text, owner: forge.record?.id });
		text = "";
		load();
	}
	async function remove(id: string) {
		await forge.delete("notes", id);
		load();
	}
</script>

<div class="forge-container" style="padding: 2rem 1.5rem;">
	<div class="head">
		<div>
			<h1>Dashboard</h1>
			<p class="forge-muted">Signed in as {forge.record?.email}</p>
		</div>
		<Badge variant="secondary">realtime</Badge>
	</div>

	{#if missing}
		<Card title="Create a 'notes' collection">
			<p class="forge-muted">
				This demo expects a <code>notes</code> collection with a <code>text</code> field and an
				<code>owner</code> relation to <code>users</code>. Create it in the
				<a href="/_/">admin dashboard</a> (or let Claude build it over MCP), then reload.
			</p>
		</Card>
	{:else}
		<Card title="Notes">
			<div class="row" style="gap: 0.5rem; margin-bottom: 1rem;">
				<Input bind:value={text} placeholder="Write a note…" onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && add()} />
				<Button onclick={add}>Add</Button>
			</div>
			{#if loading}
				<Spinner />
			{:else}
				<Table
					columns={[{ key: "text", label: "Note" }, { key: "created", label: "Created" }, { key: "_a", label: "" }]}
					rows={notes}
				>
					{#snippet cell({ row, column })}
						{#if column.key === "_a"}
							<Button variant="ghost" size="sm" onclick={() => remove(row.id as string)}>Delete</Button>
						{:else if column.key === "created"}
							<span class="forge-muted">{String(row.created).slice(0, 16)}</span>
						{:else}
							{row[column.key]}
						{/if}
					{/snippet}
				</Table>
			{/if}
		</Card>
	{/if}
</div>

<style>
	.head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 1.5rem; }
	h1 { margin: 0; font-size: 1.5rem; }
	code { background: hsl(var(--muted)); padding: 1px 5px; border-radius: 4px; font-size: 0.85em; }
	a { text-decoration: underline; }
</style>
