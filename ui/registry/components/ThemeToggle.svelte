<script lang="ts">
	import { onMount } from "svelte";
	let theme = $state<"light" | "dark">("light");

	onMount(() => {
		const saved = localStorage.getItem("theme");
		const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
		theme = (saved as "light" | "dark") ?? (prefersDark ? "dark" : "light");
		apply();
	});
	function apply() {
		document.documentElement.setAttribute("data-theme", theme);
		document.documentElement.classList.toggle("dark", theme === "dark");
	}
	function toggle() {
		theme = theme === "dark" ? "light" : "dark";
		localStorage.setItem("theme", theme);
		apply();
	}
</script>

<button class="theme-toggle" onclick={toggle} aria-label="Toggle theme" title="Toggle theme">
	{theme === "dark" ? "☀" : "☾"}
</button>

<style>
	.theme-toggle {
		display: inline-flex; align-items: center; justify-content: center;
		width: 2.25rem; height: 2.25rem; border-radius: calc(var(--radius) - 2px);
		border: 1px solid hsl(var(--border)); background: hsl(var(--background));
		color: hsl(var(--foreground)); cursor: pointer; font-size: 1rem;
	}
	.theme-toggle:hover { background: hsl(var(--accent)); }
</style>
