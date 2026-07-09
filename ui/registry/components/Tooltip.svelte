<script lang="ts">
	import { cn } from "./utils";
	import type { Snippet } from "svelte";
	interface Props { text: string; class?: string; children?: Snippet; }
	let { text, class: klass = "", children }: Props = $props();
	let show = $state(false);
</script>

<span
	class={cn("tooltip-wrap", klass)}
	onmouseenter={() => (show = true)}
	onmouseleave={() => (show = false)}
	onfocusin={() => (show = true)}
	onfocusout={() => (show = false)}
	role="tooltip"
>
	{@render children?.()}
	{#if show}<span class="tooltip">{text}</span>{/if}
</span>

<style>
	.tooltip-wrap { position: relative; display: inline-flex; }
	.tooltip {
		position: absolute; bottom: calc(100% + 0.4rem); left: 50%; transform: translateX(-50%);
		background: hsl(var(--foreground)); color: hsl(var(--background));
		font-size: 0.75rem; padding: 0.25rem 0.5rem; border-radius: calc(var(--radius) - 3px);
		white-space: nowrap; z-index: 60; pointer-events: none; animation: tip 0.1s ease;
	}
	@keyframes tip { from { opacity: 0; } }
</style>
