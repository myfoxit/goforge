<script lang="ts">
	import { cn } from "./utils";
	import { trapFocus } from "./utils";
	import type { Snippet } from "svelte";

	interface Props {
		open?: boolean;
		side?: "left" | "right";
		title?: string;
		class?: string;
		children?: Snippet;
	}
	let { open = $bindable(false), side = "right", title, class: klass = "", children }: Props = $props();
	function close() {
		open = false;
	}
</script>

{#if open}
	<div
		class="sheet-overlay"
		role="button"
		tabindex="-1"
		aria-label="Close"
		onclick={close}
		onkeydown={(e) => e.key === "Enter" && close()}
	></div>
	<aside
		class={cn("sheet", `sheet-${side}`, klass)}
		use:trapFocus
		onkeydown={(e) => e.key === "Escape" && close()}
	>
		{#if title}<h2 class="sheet-title">{title}</h2>{/if}
		<div class="sheet-body">{@render children?.()}</div>
	</aside>
{/if}

<style>
	.sheet-overlay { position: fixed; inset: 0; background: hsl(0 0% 0% / 0.5); z-index: 50; border: 0; }
	.sheet {
		position: fixed;
		top: 0;
		bottom: 0;
		z-index: 51;
		width: min(24rem, 90vw);
		background: hsl(var(--background));
		color: hsl(var(--foreground));
		padding: 1.5rem;
		box-shadow: 0 0 40px hsl(0 0% 0% / 0.25);
		overflow-y: auto;
	}
	.sheet-right { right: 0; border-left: 1px solid hsl(var(--border)); animation: slide-l 0.2s ease; }
	.sheet-left { left: 0; border-right: 1px solid hsl(var(--border)); animation: slide-r 0.2s ease; }
	.sheet-title { font-size: 1.05rem; font-weight: 600; margin-bottom: 1rem; }
	@keyframes slide-l { from { transform: translateX(100%); } }
	@keyframes slide-r { from { transform: translateX(-100%); } }
</style>
