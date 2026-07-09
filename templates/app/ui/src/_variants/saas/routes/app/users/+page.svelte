<script lang="ts">
	import { onMount } from "svelte";
	import { Card, Button, Input, Table, Badge, Spinner, Dialog, Checkbox, Alert, toaster } from "$ui";
	import { api, type Role } from "$lib/api";
	import { shortDate } from "$lib/format";

	type User = Record<string, any>;

	let users = $state<User[]>([]);
	let roles = $state<Role[]>([]);
	let loading = $state(true);
	let denied = $state(false);
	let query = $state("");
	let page = $state(1);
	const perPage = 25;
	let totalPages = $state(1);
	let totalItems = $state(0);
	let searchTimer: ReturnType<typeof setTimeout>;

	// role editor
	let editing = $state<User | null>(null);
	let picked = $state<Record<string, boolean>>({});
	let savingRoles = $state(false);

	onMount(async () => {
		try {
			roles = (await api.listRoles()).items;
		} catch {
			/* roles are best-effort */
		}
		await load();
	});

	async function load() {
		loading = true;
		try {
			const filter = query.trim() ? `email ~ ${JSON.stringify(query.trim())}` : "";
			const res = await api.listUsers({ page, perPage, filter });
			users = res.items;
			totalPages = res.totalPages;
			totalItems = res.totalItems;
			denied = false;
		} catch (e) {
			if ((e as { status?: number }).status === 403) denied = true;
			else toaster.error("Couldn't load users", (e as Error).message);
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
	function go(p: number) {
		if (p < 1 || p > totalPages) return;
		page = p;
		load();
	}

	function roleNames(u: User): string[] {
		const expanded = u.expand?.roles as Role[] | undefined;
		if (expanded) return expanded.map((r) => r.name);
		return [];
	}

	function openEditor(u: User) {
		editing = u;
		const current: string[] = Array.isArray(u.roles) ? u.roles : [];
		picked = Object.fromEntries(roles.map((r) => [r.id, current.includes(r.id)]));
	}
	async function saveRoles() {
		if (!editing) return;
		savingRoles = true;
		try {
			const ids = roles.filter((r) => picked[r.id]).map((r) => r.id);
			await api.setUserRoles(editing.id, ids);
			editing = null;
			await load();
			toaster.success("Roles updated");
		} catch (e) {
			toaster.error("Couldn't update roles", (e as Error).message);
		} finally {
			savingRoles = false;
		}
	}

	const columns = [
		{ key: "email", label: "Email" },
		{ key: "roles", label: "Roles" },
		{ key: "verified", label: "Verified" },
		{ key: "created", label: "Joined" },
		{ key: "_a", label: "", align: "right" as const },
	];
</script>

<div class="head">
	<div>
		<h1 class="h1">Users</h1>
		<p class="forge-muted sub">{totalItems} user{totalItems === 1 ? "" : "s"} · manage roles and access.</p>
	</div>
</div>

{#if denied}
	<Alert variant="destructive">You don't have permission to manage users.</Alert>
{:else}
	<div class="toolbar">
		<Input bind:value={query} oninput={onSearch} placeholder="Search by email…" class="search" />
	</div>

	{#if loading}
		<div class="loading"><Spinner /></div>
	{:else}
		<Table {columns} rows={users} empty="No users match.">
			{#snippet cell({ row, column })}
				{#if column.key === "roles"}
					<span class="roles">
						{#each roleNames(row) as r}<Badge variant="secondary">{r}</Badge>{/each}
						{#if roleNames(row).length === 0}<span class="forge-muted">—</span>{/if}
					</span>
				{:else if column.key === "verified"}
					{#if row.verified}<Badge variant="success">Yes</Badge>{:else}<Badge variant="outline">No</Badge>{/if}
				{:else if column.key === "created"}
					<span class="forge-muted">{shortDate(row.created)}</span>
				{:else if column.key === "_a"}
					<Button variant="ghost" size="sm" onclick={() => openEditor(row)}>Edit roles</Button>
				{:else}
					{row[column.key]}
				{/if}
			{/snippet}
		</Table>

		{#if totalPages > 1}
			<div class="pager">
				<Button variant="outline" size="sm" disabled={page <= 1} onclick={() => go(page - 1)}>Previous</Button>
				<span class="forge-muted small">Page {page} of {totalPages}</span>
				<Button variant="outline" size="sm" disabled={page >= totalPages} onclick={() => go(page + 1)}>Next</Button>
			</div>
		{/if}
	{/if}
{/if}

<Dialog open={!!editing} title="Edit roles" description={editing?.email} onclose={() => (editing = null)}>
	{#if roles.length === 0}
		<p class="forge-muted">No roles defined yet. Create roles in the <a href="/_/">admin</a>.</p>
	{:else}
		<div class="rolelist">
			{#each roles as r}
				<label class="roleopt">
					<Checkbox bind:checked={picked[r.id]} />
					<span>{r.name}</span>
				</label>
			{/each}
		</div>
	{/if}
	{#snippet footer()}
		<Button variant="outline" onclick={() => (editing = null)}>Cancel</Button>
		<Button loading={savingRoles} onclick={saveRoles} disabled={roles.length === 0}>Save</Button>
	{/snippet}
</Dialog>

<style>
	.head { margin-bottom: 1.25rem; }
	.h1 { font-size: 1.5rem; font-weight: 700; margin: 0; }
	.sub { margin: 0.25rem 0 0; }
	.toolbar { margin-bottom: 1rem; }
	:global(.search) { max-width: 280px; }
	.loading { display: flex; justify-content: center; padding: 3rem; }
	.roles { display: inline-flex; gap: 0.35rem; flex-wrap: wrap; }
	.pager { display: flex; align-items: center; justify-content: center; gap: 1rem; margin-top: 1.25rem; }
	.small { font-size: 0.8125rem; }
	.rolelist { display: flex; flex-direction: column; gap: 0.6rem; }
	.roleopt { display: flex; align-items: center; gap: 0.6rem; cursor: pointer; }
	a { text-decoration: underline; }
</style>
