// Minimal class-name combiner — no clsx/tailwind-merge dependency. Accepts the
// same shapes as Svelte's `class` attribute (strings, arrays, objects).
export type ClassInput =
	| string
	| number
	| bigint
	| boolean
	| null
	| undefined
	| ClassInput[]
	| Record<string, unknown>;

export function cn(...parts: ClassInput[]): string {
	const out: string[] = [];
	for (const p of parts) {
		if (!p) continue;
		if (typeof p === "string" || typeof p === "number") out.push(String(p));
		else if (Array.isArray(p)) out.push(cn(...p));
		else if (typeof p === "object") {
			for (const [k, v] of Object.entries(p)) if (v) out.push(k);
		}
	}
	return out.join(" ");
}

// Focus-trap + escape handler used by overlay components (dialog, sheet).
export function trapFocus(node: HTMLElement) {
	const selector =
		'a[href], button:not([disabled]), textarea, input, select, [tabindex]:not([tabindex="-1"])';
	const focusables = () => Array.from(node.querySelectorAll<HTMLElement>(selector));

	function onKey(e: KeyboardEvent) {
		if (e.key !== "Tab") return;
		const items = focusables();
		if (items.length === 0) return;
		const first = items[0];
		const last = items[items.length - 1];
		if (e.shiftKey && document.activeElement === first) {
			e.preventDefault();
			last.focus();
		} else if (!e.shiftKey && document.activeElement === last) {
			e.preventDefault();
			first.focus();
		}
	}
	node.addEventListener("keydown", onKey);
	focusables()[0]?.focus();
	return {
		destroy() {
			node.removeEventListener("keydown", onKey);
		},
	};
}
