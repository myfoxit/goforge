<script lang="ts">
	import { onMount } from "svelte";
	import { Card, Button, Input, Select, Field, Badge, Table, Spinner, Dialog, toaster } from "$ui";
	import { api, type Org, type Member } from "$lib/api";
	import { session } from "$lib/session.svelte";

	let orgs = $state<Org[]>([]);
	let loading = $state(true);
	let selected = $state<Org | null>(null);
	let members = $state<Member[]>([]);
	let membersLoading = $state(false);

	let showCreate = $state(false);
	let orgName = $state("");
	let creating = $state(false);

	let inviteEmail = $state("");
	let inviteRole = $state<"member" | "admin">("member");
	let inviting = $state(false);

	onMount(loadOrgs);

	async function loadOrgs() {
		loading = true;
		try {
			const res = await api.listOrgs();
			orgs = res.items;
			if (orgs.length && !selected) select(orgs[0]);
		} catch (e) {
			toaster.error("Couldn't load organizations", (e as Error).message);
		} finally {
			loading = false;
		}
	}

	async function select(org: Org) {
		selected = org;
		membersLoading = true;
		try {
			members = (await api.members(org.id)).items;
		} catch {
			members = [];
		} finally {
			membersLoading = false;
		}
	}

	async function create() {
		if (!orgName.trim()) return;
		creating = true;
		try {
			const org = await api.createOrg(orgName.trim());
			orgName = "";
			showCreate = false;
			await loadOrgs();
			const fresh = orgs.find((o) => o.id === org.id);
			if (fresh) select(fresh);
			toaster.success("Organization created", org.name);
		} catch (e) {
			toaster.error("Couldn't create", (e as Error).message);
		} finally {
			creating = false;
		}
	}

	async function invite(e: Event) {
		e.preventDefault();
		const org = selected;
		if (!org) return;
		inviting = true;
		try {
			await api.invite(org.id, inviteEmail, inviteRole);
			toaster.success("Invitation sent", inviteEmail);
			inviteEmail = "";
		} catch (err) {
			toaster.error("Couldn't invite", (err as Error).message);
		} finally {
			inviting = false;
		}
	}

	async function leave() {
		const org = selected;
		if (!org) return;
		if (!confirm(`Leave ${org.name}?`)) return;
		try {
			await api.leaveOrg(org.id);
			selected = null;
			await loadOrgs();
			toaster.success("You left the organization");
		} catch (err) {
			toaster.error("Couldn't leave", (err as Error).message);
		}
	}

	const isOwner = $derived(!!selected && selected.owner === session.id);
	const memberColumns = [
		{ key: "email", label: "Member" },
		{ key: "role", label: "Role" },
	];
</script>

<div class="head">
	<div>
		<h1 class="h1">Team</h1>
		<p class="forge-muted sub">Organizations you belong to and their members.</p>
	</div>
	<Button onclick={() => (showCreate = true)}>New organization</Button>
</div>

{#if loading}
	<div class="loading"><Spinner /></div>
{:else if orgs.length === 0}
	<Card title="No organizations yet">
		<p class="forge-muted">Create one to invite teammates and scope data to a tenant.</p>
		<div class="mt"><Button onclick={() => (showCreate = true)}>Create organization</Button></div>
	</Card>
{:else}
	<div class="split">
		<aside class="orglist">
			{#each orgs as org}
				<button class="orgitem" class:active={selected?.id === org.id} onclick={() => select(org)}>
					<span class="oname">{org.name}</span>
					{#if org.owner === session.id}<Badge variant="secondary">Owner</Badge>{/if}
				</button>
			{/each}
		</aside>

		<section class="detail">
			{#if selected}
				<Card>
					{#snippet header()}
						<div class="dhead">
							<div>
								<h3 class="card-title">{selected?.name}</h3>
								<p class="forge-muted small">/{selected?.slug}</p>
							</div>
							{#if !isOwner}<Button variant="outline" size="sm" onclick={leave}>Leave</Button>{/if}
						</div>
					{/snippet}

					{#if membersLoading}
						<Spinner />
					{:else}
						<Table columns={memberColumns} rows={members} empty="No members.">
							{#snippet cell({ row, column })}
								{#if column.key === "email"}
									{row.expand?.user?.email ?? row.user}
								{:else}
									<Badge variant={row.role === "owner" ? "default" : "secondary"}>{row.role}</Badge>
								{/if}
							{/snippet}
						</Table>
					{/if}
				</Card>

				<Card title="Invite a teammate" description="They'll get an email to join this organization.">
					<form onsubmit={invite} class="inviteform">
						<Input type="email" bind:value={inviteEmail} placeholder="teammate@example.com" required />
						<Select bind:value={inviteRole} options={[{ value: "member", label: "Member" }, { value: "admin", label: "Admin" }]} />
						<Button type="submit" loading={inviting}>Invite</Button>
					</form>
				</Card>
			{/if}
		</section>
	</div>
{/if}

<Dialog bind:open={showCreate} title="New organization">
	<Field label="Name" for="org-name">
		<Input id="org-name" bind:value={orgName} placeholder="Acme Inc." required />
	</Field>
	{#snippet footer()}
		<Button variant="outline" onclick={() => (showCreate = false)}>Cancel</Button>
		<Button loading={creating} onclick={create}>Create</Button>
	{/snippet}
</Dialog>

<style>
	.head { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; margin-bottom: 1.25rem; }
	.h1 { font-size: 1.5rem; font-weight: 700; margin: 0; }
	.sub { margin: 0.25rem 0 0; }
	.loading { display: flex; justify-content: center; padding: 3rem; }
	.mt { margin-top: 1rem; }
	.split { display: grid; grid-template-columns: 220px 1fr; gap: 1.25rem; align-items: start; }
	.orglist { display: flex; flex-direction: column; gap: 0.35rem; }
	.orgitem {
		display: flex; align-items: center; justify-content: space-between; gap: 0.5rem;
		padding: 0.6rem 0.75rem; border: 1px solid hsl(var(--border)); border-radius: var(--radius);
		background: hsl(var(--card)); cursor: pointer; text-align: left; color: inherit;
	}
	.orgitem:hover { border-color: hsl(var(--ring)); }
	.orgitem.active { border-color: hsl(var(--primary)); }
	.oname { font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.detail { display: flex; flex-direction: column; gap: 1.25rem; }
	.dhead { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; }
	.small { font-size: 0.8125rem; }
	.inviteform { display: flex; gap: 0.5rem; flex-wrap: wrap; }
	.inviteform :global(input) { flex: 1; min-width: 180px; }
	@media (max-width: 760px) { .split { grid-template-columns: 1fr; } }
</style>
