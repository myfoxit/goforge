// Typed helpers for the SaaS surfaces — organizations, member invites, user
// administration and MFA. Everything is a thin wrapper over the shared GoForge
// client so you can see exactly which endpoint each screen hits.
import { forge } from "$lib/goforge";

export interface Org {
	id: string;
	name: string;
	slug: string;
	owner: string;
	members: string[];
	created?: string;
}

export type OrgRole = "owner" | "admin" | "member";

export interface Member {
	id: string;
	org: string;
	user: string;
	role: OrgRole;
	expand?: { user?: { id: string; email: string } };
}

export interface Role {
	id: string;
	name: string;
}

export const api = {
	// ---- organizations (pkg/orgs) ----
	listOrgs() {
		return forge.list<Org>("orgs", { sort: "-created", perPage: 100 });
	},
	createOrg(name: string, slug?: string) {
		return forge.send<Org>("POST", "/api/orgs", { name, slug });
	},
	invite(orgId: string, email: string, role: "member" | "admin") {
		return forge.send<{ sent: boolean; email: string }>(
			"POST",
			`/api/orgs/${orgId}/invite`,
			{ email, role },
		);
	},
	acceptInvite(token: string) {
		return forge.send<Org>("POST", "/api/orgs/accept-invite", { token });
	},
	leaveOrg(orgId: string) {
		return forge.send("POST", `/api/orgs/${orgId}/leave`);
	},
	members(orgId: string) {
		return forge.list<Member>("org_members", {
			filter: `org="${orgId}"`,
			expand: "user",
			perPage: 200,
		});
	},

	// ---- users & roles (pkg/perm) — admin only, gated by collection rules ----
	listUsers(opts: { page?: number; perPage?: number; filter?: string } = {}) {
		return forge.list("users", { sort: "-created", perPage: 50, expand: "roles", ...opts });
	},
	listRoles() {
		return forge.list<Role>("_roles", { perPage: 200 });
	},
	setUserRoles(userId: string, roleIds: string[]) {
		return forge.update("users", userId, { roles: roleIds });
	},

	// ---- account ----
	updateAccount(userId: string, data: Record<string, unknown>) {
		return forge.update("users", userId, data);
	},

	// ---- MFA (pkg/auth) ----
	mfaSetup() {
		return forge.send<{ secret: string; otpauthURL: string }>("POST", "/api/mfa/setup");
	},
	mfaActivate(code: string) {
		return forge.send("POST", "/api/mfa/activate", { code });
	},
	mfaDisable(password: string) {
		return forge.send("POST", "/api/mfa/disable", { password });
	},
};
