<script lang="ts">
	import { Card, Field, Input, Button, Badge, Alert, toaster } from "$ui";
	import { forge } from "$lib/goforge";
	import { api } from "$lib/api";
	import { session } from "$lib/session.svelte";
	import { goto } from "$app/navigation";

	const verified = $derived(!!session.user?.verified);

	// --- email ---
	let newEmail = $state("");
	let emailBusy = $state(false);
	async function changeEmail(e: Event) {
		e.preventDefault();
		emailBusy = true;
		try {
			await forge.send("POST", "/api/collections/users/request-email-change", { newEmail });
			toaster.success("Confirmation sent", `Check ${newEmail} to confirm the change.`);
			newEmail = "";
		} catch (err) {
			toaster.error("Couldn't change email", (err as Error).message);
		} finally {
			emailBusy = false;
		}
	}

	// --- password ---
	let pw = $state("");
	let pwConfirm = $state("");
	let pwBusy = $state(false);
	async function changePassword(e: Event) {
		e.preventDefault();
		if (pw !== pwConfirm) {
			toaster.error("Passwords do not match");
			return;
		}
		pwBusy = true;
		try {
			await api.updateAccount(session.id, { password: pw, passwordConfirm: pwConfirm });
			pw = "";
			pwConfirm = "";
			toaster.success("Password updated");
			await session.refresh();
		} catch (err) {
			toaster.error("Couldn't update password", (err as Error).message);
		} finally {
			pwBusy = false;
		}
	}

	// --- MFA ---
	let secret = $state("");
	let otpauth = $state("");
	let mfaCode = $state("");
	let disablePw = $state("");
	let mfaBusy = $state(false);

	async function startMFA() {
		mfaBusy = true;
		try {
			const res = await api.mfaSetup();
			secret = res.secret;
			otpauth = res.otpauthURL;
		} catch (err) {
			toaster.error("Couldn't start setup", (err as Error).message);
		} finally {
			mfaBusy = false;
		}
	}
	async function activateMFA(e: Event) {
		e.preventDefault();
		mfaBusy = true;
		try {
			await api.mfaActivate(mfaCode);
			secret = "";
			otpauth = "";
			mfaCode = "";
			toaster.success("Two-factor enabled");
		} catch (err) {
			toaster.error("Invalid code", (err as Error).message);
		} finally {
			mfaBusy = false;
		}
	}
	async function disableMFA(e: Event) {
		e.preventDefault();
		mfaBusy = true;
		try {
			await api.mfaDisable(disablePw);
			disablePw = "";
			toaster.success("Two-factor disabled");
		} catch (err) {
			toaster.error("Couldn't disable", (err as Error).message);
		} finally {
			mfaBusy = false;
		}
	}

	// --- danger ---
	async function resend() {
		try {
			await forge.requestVerification("users", session.email);
			toaster.success("Verification email sent");
		} catch (err) {
			toaster.error("Couldn't send", (err as Error).message);
		}
	}
	async function deleteAccount() {
		if (!confirm("Delete your account permanently? This cannot be undone.")) return;
		try {
			await forge.delete("users", session.id);
			session.logout();
			goto("/");
		} catch (err) {
			toaster.error("Couldn't delete account", (err as Error).message);
		}
	}
</script>

<h1 class="h1">Account</h1>
<p class="forge-muted sub">Manage your profile and security.</p>

<div class="stack">
	<Card title="Profile">
		<div class="row">
			<div>
				<div class="lbl">Email</div>
				<div class="val">{session.email}</div>
			</div>
			{#if verified}
				<Badge variant="success">Verified</Badge>
			{:else}
				<div class="unverified">
					<Badge variant="warning">Unverified</Badge>
					<Button size="sm" variant="outline" onclick={resend}>Resend</Button>
				</div>
			{/if}
		</div>
		<form onsubmit={changeEmail} class="forge-stack mt">
			<Field label="Change email" for="newEmail" hint="We'll email the new address to confirm.">
				<Input id="newEmail" type="email" bind:value={newEmail} placeholder="new@example.com" required />
			</Field>
			<div><Button type="submit" loading={emailBusy} variant="outline">Send confirmation</Button></div>
		</form>
	</Card>

	<Card title="Password">
		<form onsubmit={changePassword} class="forge-stack">
			<Field label="New password" for="pw" hint="At least 10 characters.">
				<Input id="pw" type="password" bind:value={pw} required minlength={10} />
			</Field>
			<Field label="Confirm new password" for="pwc">
				<Input id="pwc" type="password" bind:value={pwConfirm} required />
			</Field>
			<div><Button type="submit" loading={pwBusy}>Update password</Button></div>
		</form>
	</Card>

	<Card title="Two-factor authentication" description="Protect your account with a TOTP authenticator app.">
		{#if secret}
			<Alert>Scan or enter this secret in your authenticator, then confirm with a code.</Alert>
			<div class="secret">
				<code>{secret}</code>
				<a class="otp" href={otpauth}>Open in authenticator</a>
			</div>
			<form onsubmit={activateMFA} class="forge-stack mt">
				<Field label="Enter the 6-digit code" for="mfaCode">
					<Input id="mfaCode" inputmode="numeric" bind:value={mfaCode} placeholder="123456" required />
				</Field>
				<div><Button type="submit" loading={mfaBusy}>Activate</Button></div>
			</form>
		{:else}
			<div class="two-col">
				<div>
					<p class="forge-muted small">Enable a second factor for password logins.</p>
					<Button variant="outline" loading={mfaBusy} onclick={startMFA}>Enable two-factor</Button>
				</div>
				<form onsubmit={disableMFA} class="disable">
					<Field label="Disable (confirm password)" for="dpw">
						<Input id="dpw" type="password" bind:value={disablePw} placeholder="Current password" />
					</Field>
					<Button type="submit" variant="ghost" loading={mfaBusy} disabled={!disablePw}>Disable</Button>
				</form>
			</div>
		{/if}
	</Card>

	<Card title="Danger zone">
		<div class="row">
			<p class="forge-muted small">Permanently delete your account and its data.</p>
			<Button variant="destructive" onclick={deleteAccount}>Delete account</Button>
		</div>
	</Card>
</div>

<style>
	.h1 { font-size: 1.5rem; font-weight: 700; margin: 0; }
	.sub { margin: 0.25rem 0 1.5rem; }
	.stack { display: flex; flex-direction: column; gap: 1.25rem; max-width: 620px; }
	.row { display: flex; align-items: center; justify-content: space-between; gap: 1rem; }
	.mt { margin-top: 1.25rem; }
	.lbl { font-size: 0.8125rem; color: hsl(var(--muted-foreground)); }
	.val { font-weight: 500; }
	.unverified { display: flex; align-items: center; gap: 0.5rem; }
	.small { font-size: 0.875rem; }
	.secret { display: flex; align-items: center; justify-content: space-between; gap: 1rem; margin-top: 0.75rem; }
	.secret code { background: hsl(var(--muted)); padding: 0.4rem 0.6rem; border-radius: 6px; font-size: 0.9rem; letter-spacing: 0.05em; }
	.otp { text-decoration: underline; font-size: 0.8125rem; }
	.two-col { display: grid; grid-template-columns: 1fr 1fr; gap: 1.5rem; align-items: end; }
	.disable { display: flex; flex-direction: column; gap: 0.5rem; }
	@media (max-width: 620px) { .two-col { grid-template-columns: 1fr; } }
</style>
