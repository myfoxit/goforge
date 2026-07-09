// Minimal, dependency-free GoForge client for the browser.
// Persists the auth token and exposes typed record + auth helpers plus
// realtime subscriptions over SSE.

export interface AuthRecord {
	id: string;
	email: string;
	[key: string]: unknown;
}

export interface ListResult<T = Record<string, unknown>> {
	page: number;
	perPage: number;
	totalItems: number;
	totalPages: number;
	items: T[];
}

export interface ListOptions {
	page?: number;
	perPage?: number;
	sort?: string;
	filter?: string;
	expand?: string;
}

export interface OAuthProvider {
	name: string;
	displayName: string;
	authURL: string;
}

export interface AuthMethods {
	password: { enabled: boolean; identityFields: string[] };
	oauth2: { enabled: boolean; providers: OAuthProvider[] };
	mfa: { enabled: boolean };
}

export class ForgeError extends Error {
	status: number;
	data?: Record<string, string>;
	body?: unknown; // full parsed response body
	constructor(message: string, status: number, data?: Record<string, string>, body?: unknown) {
		super(message);
		this.status = status;
		this.data = data;
		this.body = body;
	}
}

export class GoForge {
	baseURL: string;
	token = "";
	record: AuthRecord | null = null;
	private storageKey = "gf_auth";

	constructor(baseURL = "") {
		this.baseURL = baseURL.replace(/\/$/, "");
		if (typeof localStorage !== "undefined") {
			const saved = localStorage.getItem(this.storageKey);
			if (saved) {
				try {
					const parsed = JSON.parse(saved);
					this.token = parsed.token;
					this.record = parsed.record;
				} catch {
					/* ignore */
				}
			}
		}
	}

	get isAuthenticated() {
		return !!this.token;
	}

	private persist() {
		if (typeof localStorage === "undefined") return;
		if (this.token) {
			localStorage.setItem(this.storageKey, JSON.stringify({ token: this.token, record: this.record }));
		} else {
			localStorage.removeItem(this.storageKey);
		}
	}

	async send<T = unknown>(method: string, path: string, body?: unknown, isForm = false): Promise<T> {
		const headers: Record<string, string> = {};
		if (this.token) headers["Authorization"] = `Bearer ${this.token}`;
		let payload: BodyInit | undefined;
		if (isForm) {
			payload = body as FormData;
		} else if (body !== undefined) {
			headers["Content-Type"] = "application/json";
			payload = JSON.stringify(body);
		}
		const res = await fetch(this.baseURL + path, { method, headers, body: payload });
		if (res.status === 204) return null as T;
		const text = await res.text();
		let data: unknown = null;
		try {
			data = text ? JSON.parse(text) : null;
		} catch {
			data = text;
		}
		if (!res.ok) {
			const d = data as { message?: string; data?: Record<string, string> };
			throw new ForgeError(d?.message || res.statusText, res.status, d?.data, data);
		}
		return data as T;
	}

	// ---- auth ----
	async login(collection: string, identity: string, password: string): Promise<AuthRecord> {
		let res: { token: string; record: AuthRecord };
		try {
			res = await this.send<{ token: string; record: AuthRecord }>(
				"POST",
				`/api/collections/${collection}/auth-with-password`,
				{ identity, password },
			);
		} catch (err) {
			// A password login with MFA enabled answers 401 + { mfaRequired,
			// mfaToken }. Surface the short-lived token so the caller can
			// complete the second factor via completeMFA().
			const fe = err as ForgeError;
			const body = fe?.body as { mfaRequired?: boolean; mfaToken?: string } | undefined;
			if (fe?.status === 401 && body?.mfaRequired && body.mfaToken) {
				throw new ForgeError("MFA required", 401, { mfaToken: body.mfaToken });
			}
			throw err;
		}
		this.token = res.token;
		this.record = res.record;
		this.persist();
		return res.record;
	}

	async register(collection: string, data: Record<string, unknown>): Promise<AuthRecord> {
		return this.send("POST", `/api/collections/${collection}/records`, data);
	}

	async refresh(collection: string): Promise<AuthRecord> {
		const res = await this.send<{ token: string; record: AuthRecord }>(
			"POST",
			`/api/collections/${collection}/auth-refresh`,
		);
		this.token = res.token;
		this.record = res.record;
		this.persist();
		return res.record;
	}

	// completeMFA finishes a password login that answered with mfaRequired,
	// exchanging the short-lived mfaToken + TOTP code for a full session.
	async completeMFA(collection: string, mfaToken: string, code: string): Promise<AuthRecord> {
		const res = await this.send<{ token: string; record: AuthRecord }>(
			"POST",
			`/api/mfa/verify`,
			{ mfaToken, code },
		);
		this.token = res.token;
		this.record = res.record;
		this.persist();
		return res.record;
	}

	// authMethods reports which sign-in methods a collection offers (password,
	// enabled OAuth providers, whether MFA is on) so the UI can render buttons.
	authMethods(collection: string): Promise<AuthMethods> {
		return this.send("GET", `/api/collections/${collection}/auth-methods`);
	}

	// finishOAuth consumes the "#oauthToken=..." fragment left by the OAuth
	// callback redirect, adopts the session, and cleans the URL. Returns true
	// when a token was present and the session was established.
	async finishOAuth(collection = "users"): Promise<boolean> {
		if (typeof location === "undefined") return false;
		const m = location.hash.match(/oauthToken=([^&]+)/);
		if (!m) return false;
		this.token = decodeURIComponent(m[1]);
		history.replaceState(null, "", location.pathname + location.search);
		try {
			await this.refresh(collection);
			return true;
		} catch {
			this.logout();
			return false;
		}
	}

	requestVerification(collection: string, email: string) {
		return this.send("POST", `/api/collections/${collection}/request-verification`, { email });
	}
	requestPasswordReset(collection: string, email: string) {
		return this.send("POST", `/api/collections/${collection}/request-password-reset`, { email });
	}
	confirmPasswordReset(collection: string, token: string, password: string) {
		return this.send("POST", `/api/collections/${collection}/confirm-password-reset`, {
			token,
			password,
			passwordConfirm: password,
		});
	}
	confirmVerification(collection: string, token: string) {
		return this.send("POST", `/api/collections/${collection}/confirm-verification`, { token });
	}

	logout() {
		this.token = "";
		this.record = null;
		this.persist();
	}

	oauthURL(collection: string, provider: string, redirect = "/") {
		return `${this.baseURL}/api/oauth2/${collection}/${provider}?redirect=${encodeURIComponent(redirect)}`;
	}

	// ---- records ----
	list<T = Record<string, unknown>>(collection: string, opts: ListOptions = {}): Promise<ListResult<T>> {
		const q = new URLSearchParams();
		if (opts.page) q.set("page", String(opts.page));
		if (opts.perPage) q.set("perPage", String(opts.perPage));
		if (opts.sort) q.set("sort", opts.sort);
		if (opts.filter) q.set("filter", opts.filter);
		if (opts.expand) q.set("expand", opts.expand);
		return this.send("GET", `/api/collections/${collection}/records?${q}`);
	}
	getOne<T = Record<string, unknown>>(collection: string, id: string, expand?: string): Promise<T> {
		const q = expand ? `?expand=${expand}` : "";
		return this.send("GET", `/api/collections/${collection}/records/${id}${q}`);
	}
	create<T = Record<string, unknown>>(collection: string, data: Record<string, unknown> | FormData): Promise<T> {
		return this.send("POST", `/api/collections/${collection}/records`, data, data instanceof FormData);
	}
	update<T = Record<string, unknown>>(
		collection: string,
		id: string,
		data: Record<string, unknown> | FormData,
	): Promise<T> {
		return this.send("PATCH", `/api/collections/${collection}/records/${id}`, data, data instanceof FormData);
	}
	delete(collection: string, id: string): Promise<null> {
		return this.send("DELETE", `/api/collections/${collection}/records/${id}`);
	}

	fileURL(collection: string, recordId: string, filename: string, thumb?: string) {
		const q = thumb ? `?thumb=${thumb}` : "";
		return `${this.baseURL}/api/files/${collection}/${recordId}/${filename}${q}`;
	}

	// ---- realtime ----
	// subscribe opens an SSE stream and invokes cb for each event on the given
	// topics ("posts" for a collection, "posts/<id>" for one record).
	// Returns an unsubscribe function.
	realtime(topics: string[], cb: (event: { action: string; record: Record<string, unknown> }) => void): () => void {
		const es = new EventSource(this.baseURL + "/api/realtime");
		let clientId = "";
		es.addEventListener("GF_CONNECT", async (e) => {
			clientId = JSON.parse((e as MessageEvent).data).clientId;
			await this.send("POST", "/api/realtime", { clientId, subscriptions: topics });
		});
		for (const topic of topics) {
			es.addEventListener(topic, (e) => cb(JSON.parse((e as MessageEvent).data)));
		}
		return () => es.close();
	}
}

// Shared singleton — same-origin in production, proxied in dev.
export const forge = new GoForge("");
