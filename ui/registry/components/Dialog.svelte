<script lang="ts">
	import { cn } from "./utils";
	import { trapFocus } from "./utils";
	import type { Snippet } from "svelte";

	interface Props {
		open?: boolean;
		title?: string;
		description?: string;
		class?: string;
		children?: Snippet;
		footer?: Snippet;
		onclose?: () => void;
	}
	let {
		open = $bindable(false),
		title,
		description,
		class: klass = "",
		children,
		footer,
		onclose,
	}: Props = $props();

	function close() {
		open = false;
		onclose?.();
	}
	function onKey(e: KeyboardEvent) {
		if (e.key === "Escape") close();
	}
</script>

{#if open}
	<div
		class="dialog-overlay"
		role="button"
		tabindex="-1"
		aria-label="Close dialog"
		onclick={close}
		onkeydown={(e) => e.key === "Enter" && close()}
	></div>
	<div
		class={cn("dialog", klass)}
		role="dialog"
		aria-modal="true"
		aria-label={title}
		use:trapFocus
		onkeydown={onKey}
	>
		{#if title || description}
			<div class="dialog-header">
				{#if title}<h2 class="dialog-title">{title}</h2>{/if}
				{#if description}<p class="dialog-desc">{description}</p>{/if}
			</div>
		{/if}
		<div class="dialog-body">{@render children?.()}</div>
		{#if footer}<div class="dialog-footer">{@render footer()}</div>{/if}
		<button class="dialog-x" aria-label="Close" onclick={close}>✕</button>
	</div>
{/if}

<style>
	.dialog-overlay {
		position: fixed;
		inset: 0;
		background: hsl(0 0% 0% / 0.5);
		z-index: 50;
		animation: fade 0.15s ease;
		border: 0;
	}
	.dialog {
		position: fixed;
		top: 50%;
		left: 50%;
		transform: translate(-50%, -50%);
		z-index: 51;
		width: calc(100% - 2rem);
		max-width: 30rem;
		background: hsl(var(--popover));
		color: hsl(var(--popover-foreground));
		border: 1px solid hsl(var(--border));
		border-radius: var(--radius);
		box-shadow: 0 10px 40px hsl(0 0% 0% / 0.25);
		padding: 1.5rem;
		animation: pop 0.15s ease;
	}
	.dialog-header { margin-bottom: 1rem; }
	.dialog-title { font-size: 1.125rem; font-weight: 600; }
	.dialog-desc { font-size: 0.875rem; color: hsl(var(--muted-foreground)); margin-top: 0.35rem; }
	.dialog-footer { margin-top: 1.5rem; display: flex; justify-content: flex-end; gap: 0.5rem; }
	.dialog-x {
		position: absolute;
		top: 0.9rem;
		right: 1rem;
		background: transparent;
		border: 0;
		color: hsl(var(--muted-foreground));
		cursor: pointer;
		font-size: 0.9rem;
	}
	.dialog-x:hover { color: hsl(var(--foreground)); }
	@keyframes fade { from { opacity: 0; } }
	@keyframes pop { from { opacity: 0; transform: translate(-50%, -48%) scale(0.97); } }
</style>
