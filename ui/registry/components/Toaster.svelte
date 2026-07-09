<script lang="ts">
	import { toaster } from "./toast.svelte";
</script>

<div class="toaster" aria-live="polite">
	{#each $toaster as t (t.id)}
		<div class="toast toast-{t.variant}" role="status">
			<div class="toast-body">
				<strong class="toast-title">{t.title}</strong>
				{#if t.description}<span class="toast-desc">{t.description}</span>{/if}
			</div>
			<button class="toast-x" aria-label="Dismiss" onclick={() => toaster.dismiss(t.id)}>✕</button>
		</div>
	{/each}
</div>

<style>
	.toaster {
		position: fixed;
		bottom: 1rem;
		right: 1rem;
		z-index: 100;
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		max-width: 22rem;
	}
	.toast {
		display: flex;
		align-items: start;
		gap: 0.75rem;
		background: hsl(var(--popover));
		color: hsl(var(--popover-foreground));
		border: 1px solid hsl(var(--border));
		border-left-width: 3px;
		border-radius: var(--radius);
		padding: 0.75rem 1rem;
		box-shadow: 0 6px 20px hsl(0 0% 0% / 0.15);
		animation: toast-in 0.2s ease;
	}
	.toast-default { border-left-color: hsl(var(--primary)); }
	.toast-success { border-left-color: hsl(var(--success)); }
	.toast-error { border-left-color: hsl(var(--destructive)); }
	.toast-warning { border-left-color: hsl(var(--warning)); }
	.toast-body { display: flex; flex-direction: column; gap: 0.15rem; flex: 1; }
	.toast-title { font-size: 0.875rem; font-weight: 600; }
	.toast-desc { font-size: 0.8125rem; color: hsl(var(--muted-foreground)); }
	.toast-x { background: transparent; border: 0; color: hsl(var(--muted-foreground)); cursor: pointer; }
	@keyframes toast-in { from { opacity: 0; transform: translateX(1rem); } }
</style>
