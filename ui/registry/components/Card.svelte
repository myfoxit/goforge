<script lang="ts">
	import { cn } from "./utils";
	import type { Snippet } from "svelte";
	interface Props {
		class?: string;
		title?: string;
		description?: string;
		header?: Snippet;
		footer?: Snippet;
		children?: Snippet;
	}
	let { class: klass = "", title, description, header, footer, children }: Props = $props();
</script>

<div class={cn("card", klass)}>
	{#if header || title}
		<div class="card-header">
			{#if header}{@render header()}{:else}
				{#if title}<h3 class="card-title">{title}</h3>{/if}
				{#if description}<p class="card-desc">{description}</p>{/if}
			{/if}
		</div>
	{/if}
	<div class="card-content">{@render children?.()}</div>
	{#if footer}<div class="card-footer">{@render footer()}</div>{/if}
</div>

<style>
	.card {
		background: hsl(var(--card));
		color: hsl(var(--card-foreground));
		border: 1px solid hsl(var(--border));
		border-radius: var(--radius);
		box-shadow: 0 1px 3px hsl(var(--foreground) / 0.04);
	}
	.card-header { padding: 1.25rem 1.5rem 0; }
	.card-title { font-size: 1.05rem; font-weight: 600; }
	.card-desc { font-size: 0.875rem; color: hsl(var(--muted-foreground)); margin-top: 0.25rem; }
	.card-content { padding: 1.5rem; }
	.card-footer { padding: 0 1.5rem 1.5rem; display: flex; gap: 0.5rem; }
</style>
