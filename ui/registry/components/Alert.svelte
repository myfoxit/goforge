<script lang="ts">
	import { cn } from "./utils";
	import type { Snippet } from "svelte";
	type Variant = "default" | "destructive" | "success" | "warning";
	interface Props {
		variant?: Variant;
		title?: string;
		class?: string;
		children?: Snippet;
	}
	let { variant = "default", title, class: klass = "", children }: Props = $props();
</script>

<div class={cn("alert", `alert-${variant}`, klass)} role="alert">
	{#if title}<div class="alert-title">{title}</div>{/if}
	<div class="alert-body">{@render children?.()}</div>
</div>

<style>
	.alert {
		border: 1px solid hsl(var(--border));
		border-radius: var(--radius);
		padding: 0.85rem 1rem;
		font-size: 0.875rem;
	}
	.alert-title { font-weight: 600; margin-bottom: 0.2rem; }
	.alert-default { background: hsl(var(--muted) / 0.4); }
	.alert-destructive { border-color: hsl(var(--destructive) / 0.5); background: hsl(var(--destructive) / 0.08); color: hsl(var(--destructive)); }
	.alert-success { border-color: hsl(var(--success) / 0.5); background: hsl(var(--success) / 0.08); }
	.alert-warning { border-color: hsl(var(--warning) / 0.5); background: hsl(var(--warning) / 0.1); }
</style>
