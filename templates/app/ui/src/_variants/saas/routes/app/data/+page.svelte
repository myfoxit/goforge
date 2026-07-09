<script lang="ts">
	import { onMount } from "svelte";
	import { Card, Button, Input, Select, Table, Badge, Spinner, Dialog, Field, toaster } from "$ui";
	import { forge } from "$lib/goforge";
	import { session } from "$lib/session.svelte";
	import { EXAMPLE_COLLECTION } from "$lib/nav";
	import { shortDate } from "$lib/format";

	type Row = Record<string, any>;

	let rows = $state<Row[]>([]);
	let loading = $state(true);
	let missing = $state(false);
	let view = $state<"table" | "list">("table");
	let query = $state("");
	let sort = $state("-created");
	let page = $state(1);
	const perPage = 10;
	let totalPages = $state(1);
	let totalItems = $state(0);

	let showCreate = $state(false);
	let form = $state({ name: "", status: "todo", qty: 0 });
	let saving = $state(false);
	let searchTimer: ReturnType<typeof setTimeout>;

	onMount(load);

	async function load() {
		loading = true;
		try {
			const filter = query.trim() ? `name ~ ${JSON.stringify(query.trim())}` : "";
			const res = await forge.list<Row>(EXAMPLE_COLLECTION, { page, perPage, sort, filter });
			rows = res.items;
			totalPages = res.totalPages;
			totalItems = res.totalItems;
			missing = false;
		} catch (e) {
			if ((e as { status?: number }).status === 404) missing = true;
			else toaster.error("Couldn't load items", (e as Error).message);
		} finally {
			loading = false;
		}
	}

	function onSearch() {
		clearTimeout(searchTimer);
		searchTimer = setTimeout(() => {
			page = 1;
			load();
		}, 250);
	}
	function reload() {
		page = 1;
		load();
	}
	function go(p: number) {
		if (p < 1 || p > totalPages) return;
		page = p;
		load();
	}

	async function create() {
		if (!form.name.trim()) return;
		saving = true;
		try {
			await forge.create(EXAMPLE_COLLECTION, { ...form, qty: Number(form.qty) || 0, owner: session.id });
			showCreate = false;
			form = { name: "", status: "todo", qty: 0 };
			reload();
			toaster.success("Item created");
		} catch (e) {
			toaster.error("Couldn't create", (e as Error).message);
		} finally {
			saving = false;
		}
	}
	async function remove(id: string) {
		try {
			await forge.delete(EXAMPLE_COLLECTION, id);
			load();
		} catch (e) {
			toaster.error("Couldn't delete", (e as Error).message);
		}
	}

	const columns = [
		{ key: "name", label: "Name" },
		{ key: "status", label: "Status" },
		{ key: "qty", label: "Qty", align: "right" as const },
		{ key: "created", label: "Created" },
		{ key: "_a", label: "", align: "right" as const },
	];
	const statusOptions = [
		{ value: "todo", label: "To do" },
		{ value: "active", label: "Active" },
		{ value: "done", label: "Done" },
	];
	const sortOptions = [
		{ value: "-created", label: "Newest" },
		{ value: "created", label: "Oldest" },
		{ value: "name", label: "Name A–Z" },
		{ value: "-name", label: "Name Z–A" },
		{ value: "-qty", label: "Qty high → low" },
	];
	function statusVariant(s: unknown): "success" | "default" | "secondary" {
		return s === "done" ? "success" : s === "active" ? "default" : "secondary";
	}
</script>

<div class="head">
	<div>
		<h1 class="h1">Items</h1>
		<p class="forge-muted sub">{totalItems} record{totalItems === 1 ? "" : "s"} in the <code>items</code> collection.</p>
	</div>
	<Button onclick={() => (showCreate = true)}>New item</Button>
</div>

{#if missing}
	<Card title="The 'items' collection is gone">
		<p class="forge-muted">
			This demo collection was removed in the admin. Recreate it at
			<a href="/_/">/_/</a> (fields: <code>name</code>, <code>status</code>, <code>qty</code>, <code>owner</code>),
			restart the app, or delete this page.
		</p>
	</Card>
{:else}
	<div class="toolbar">
		<Input bind:value={query} oninput={onSearch} placeholder="Search by name…" class="search" />
		<div class="tools">
			<Select bind:value={sort} options={sortOptions} onchange={reload} />
			<div class="viewtog" role="group" aria-label="View">
				<button class="vbtn" class:active={view === "table"} onclick={() => (view = "table")} aria-label="Table view">▤</button>
				<button class="vbtn" class:active={view === "list"} onclick={() => (view = "list")} aria-label="List view">▦</button>
			</div>
		</div>
	</div>

	{#if loading}
		<div class="loading"><Spinner /></div>
	{:else if view === "table"}
		<Table {columns} {rows} empty="No items match.">
			{#snippet cell({ row, column })}
				{#if column.key === "status"}
					<Badge variant={statusVariant(row.status)}>{String(row.status ?? "—")}</Badge>
				{:else if column.key === "created"}
					<span class="forge-muted">{shortDate(row.created)}</span>
				{:else if column.key === "_a"}
					<Button variant="ghost" size="sm" onclick={() => remove(row.id)}>Delete</Button>
				{:else}
					{row[column.key]}
				{/if}
			{/snippet}
		</Table>
	{:else if rows.length === 0}
		<Card><p class="forge-muted center">No items match.</p></Card>
	{:else}
		<div class="cards">
			{#each rows as row}
				<Card>
					<div class="itemrow">
						<div class="itemmain">
							<div class="itemname">{row.name}</div>
							<div class="forge-muted small">{shortDate(row.created)} · qty {row.qty ?? 0}</div>
						</div>
						<Badge variant={statusVariant(row.status)}>{String(row.status ?? "—")}</Badge>
						<Button variant="ghost" size="sm" onclick={() => remove(row.id)}>✕</Button>
					</div>
				</Card>
			{/each}
		</div>
	{/if}

	{#if totalPages > 1}
		<div class="pager">
			<Button variant="outline" size="sm" disabled={page <= 1} onclick={() => go(page - 1)}>Previous</Button>
			<span class="forge-muted small">Page {page} of {totalPages}</span>
			<Button variant="outline" size="sm" disabled={page >= totalPages} onclick={() => go(page + 1)}>Next</Button>
		</div>
	{/if}
{/if}

<Dialog bind:open={showCreate} title="New item">
	<form onsubmit={(e) => { e.preventDefault(); create(); }} class="forge-stack" id="create-item">
		<Field label="Name" for="i-name">
			<Input id="i-name" bind:value={form.name} placeholder="Widget" required />
		</Field>
		<Field label="Status" for="i-status">
			<Select bind:value={form.status} options={statusOptions} />
		</Field>
		<Field label="Quantity" for="i-qty">
			<Input id="i-qty" type="number" bind:value={form.qty} min={0} />
		</Field>
	</form>
	{#snippet footer()}
		<Button variant="outline" onclick={() => (showCreate = false)}>Cancel</Button>
		<Button loading={saving} onclick={create}>Create</Button>
	{/snippet}
</Dialog>

<style>
	.head { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; margin-bottom: 1.25rem; }
	.h1 { font-size: 1.5rem; font-weight: 700; margin: 0; }
	.sub { margin: 0.25rem 0 0; }
	.toolbar { display: flex; align-items: center; justify-content: space-between; gap: 0.75rem; margin-bottom: 1rem; flex-wrap: wrap; }
	:global(.search) { max-width: 280px; }
	.tools { display: flex; align-items: center; gap: 0.5rem; }
	.viewtog { display: inline-flex; border: 1px solid hsl(var(--border)); border-radius: calc(var(--radius) - 2px); overflow: hidden; }
	.vbtn { border: 0; background: transparent; padding: 0.4rem 0.6rem; cursor: pointer; color: hsl(var(--muted-foreground)); }
	.vbtn.active { background: hsl(var(--accent)); color: hsl(var(--foreground)); }
	.loading { display: flex; justify-content: center; padding: 3rem; }
	.cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 0.85rem; }
	.itemrow { display: flex; align-items: center; gap: 0.75rem; }
	.itemmain { flex: 1; min-width: 0; }
	.itemname { font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.small { font-size: 0.8125rem; }
	.center { text-align: center; padding: 1.5rem; }
	.pager { display: flex; align-items: center; justify-content: center; gap: 1rem; margin-top: 1.25rem; }
	code { background: hsl(var(--muted)); padding: 1px 5px; border-radius: 4px; font-size: 0.85em; }
	a { text-decoration: underline; }
</style>
