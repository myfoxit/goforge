// Minimal class-name combiner — no clsx/tailwind-merge dependency.
export function cn(...parts: Array<string | false | null | undefined>): string {
	return parts.filter(Boolean).join(" ");
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
