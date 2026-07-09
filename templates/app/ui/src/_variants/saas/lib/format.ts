// Tiny presentation helpers used across the app shell.

export function initials(nameOrEmail: string): string {
	const s = (nameOrEmail || "").trim();
	if (!s) return "?";
	const parts = s.split(/[@\s._-]+/).filter(Boolean);
	return ((parts[0]?.[0] ?? "?") + (parts[1]?.[0] ?? "")).toUpperCase();
}

export function shortDate(v: unknown): string {
	const s = String(v ?? "");
	return s ? s.slice(0, 16).replace("T", " ") : "";
}

export function fromNow(v: unknown): string {
	const s = String(v ?? "");
	if (!s) return "";
	const then = Date.parse(s.replace(" ", "T"));
	if (Number.isNaN(then)) return shortDate(v);
	const secs = Math.floor((Date.now() - then) / 1000);
	if (secs < 60) return "just now";
	const mins = Math.floor(secs / 60);
	if (mins < 60) return `${mins}m ago`;
	const hrs = Math.floor(mins / 60);
	if (hrs < 24) return `${hrs}h ago`;
	const days = Math.floor(hrs / 24);
	if (days < 30) return `${days}d ago`;
	return shortDate(v);
}
