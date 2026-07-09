<script lang="ts">
	import { cn } from "./utils";
	import type { Snippet } from "svelte";

	interface Props {
		class?: string;
		align?: "start" | "end";
		trigger?: Snippet;
		children?: Snippet<[() => void]>;
	}
	let { class: klass = "", align = "start", trigger, children }: Props = $props();
	let open = $state(false);
	let root: HTMLElement;

	function close() {
		open = false;
	}
	function onWindowClick(e: MouseEvent) {
		if (open && root && !root.contains(e.target as Node)) close();
	}
</script>

<svelte:window onclick={onWindowClick} />

<div class={cn("dropdown", klass)} bind:this={root}>
	<button class="dropdown-trigger" onclick={() => (open = !open)} aria-haspopup="menu" aria-expanded={open}>
		{@render trigger?.()}
	</button>
	{#if open}
		<div class="dropdown-menu" class:end={align === "end"} role="menu">
			{@render children?.(close)}
		</div>
	{/if}
</div>

<style>
	.dropdown { position: relative; display: inline-block; }
	.dropdown-trigger { background: transparent; border: 0; cursor: pointer; padding: 0; color: inherit; }
	.dropdown-menu {
		position: absolute;
		top: calc(100% + 0.35rem);
		left: 0;
		min-width: 11rem;
		background: hsl(var(--popover));
		color: hsl(var(--popover-foreground));
		border: 1px solid hsl(var(--border));
		border-radius: var(--radius);
		box-shadow: 0 8px 24px hsl(0 0% 0% / 0.15);
		padding: 0.25rem;
		z-index: 40;
		animation: dd-in 0.12s ease;
	}
	.dropdown-menu.end { left: auto; right: 0; }
	:global(.dropdown-item) {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		width: 100%;
		padding: 0.4rem 0.6rem;
		font-size: 0.875rem;
		border: 0;
		background: transparent;
		color: inherit;
		border-radius: calc(var(--radius) - 3px);
		cursor: pointer;
		text-align: left;
	}
	:global(.dropdown-item:hover) { background: hsl(var(--accent)); color: hsl(var(--accent-foreground)); }
	:global(.dropdown-separator) { height: 1px; background: hsl(var(--border)); margin: 0.25rem 0; }
	@keyframes dd-in { from { opacity: 0; transform: translateY(-0.25rem); } }
</style>
