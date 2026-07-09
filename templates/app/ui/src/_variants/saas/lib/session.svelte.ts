// Reactive auth session shared across the app shell. Wraps the GoForge client
// so Svelte components re-render whenever the signed-in user changes.
import { forge, type AuthRecord } from "$lib/goforge";

class Session {
	user = $state<AuthRecord | null>(forge.record);

	get isAuthed(): boolean {
		return !!this.user;
	}
	get email(): string {
		return (this.user?.email as string) ?? "";
	}
	get id(): string {
		return this.user?.id ?? "";
	}
	// A user is treated as an admin when they carry any role (the perm module
	// gives ordinary users an empty roles list; superusers sign in separately).
	get isAdmin(): boolean {
		const roles = this.user?.roles;
		return Array.isArray(roles) && roles.length > 0;
	}

	async login(email: string, password: string): Promise<AuthRecord> {
		const rec = await forge.login("users", email, password);
		this.user = rec;
		return rec;
	}
	async completeMFA(mfaToken: string, code: string): Promise<AuthRecord> {
		const rec = await forge.completeMFA("users", mfaToken, code);
		this.user = rec;
		return rec;
	}
	async register(email: string, password: string): Promise<AuthRecord> {
		await forge.register("users", { email, password, passwordConfirm: password });
		return this.login(email, password);
	}
	async refresh(): Promise<void> {
		try {
			this.user = await forge.refresh("users");
		} catch {
			this.logout();
		}
	}
	// Adopt a session handed back by the OAuth callback (#oauthToken fragment).
	async finishOAuth(): Promise<boolean> {
		const ok = await forge.finishOAuth("users");
		if (ok) this.user = forge.record;
		return ok;
	}
	sync(): void {
		this.user = forge.record;
	}
	logout(): void {
		forge.logout();
		this.user = null;
	}
}

export const session = new Session();
