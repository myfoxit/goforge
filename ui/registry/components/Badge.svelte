<script lang="ts">
	import { cn } from "./utils";
	import type { Snippet } from "svelte";
	type Variant = "default" | "secondary" | "destructive" | "success" | "warning" | "outline";
	interface Props {
		variant?: Variant;
		class?: string;
		children?: Snippet;
	}
	let { variant = "default", class: klass = "", children }: Props = $props();
</script>

<span class={cn("badge", `badge-${variant}`, klass)}>{@render children?.()}</span>

<style>
	.badge {
		display: inline-flex;
		align-items: center;
		border-radius: 9999px;
		padding: 0.125rem 0.625rem;
		font-size: 0.75rem;
		font-weight: 500;
		line-height: 1.35;
		border: 1px solid transparent;
	}
	.badge-default { background: hsl(var(--primary)); color: hsl(var(--primary-foreground)); }
	.badge-secondary { background: hsl(var(--secondary)); color: hsl(var(--secondary-foreground)); }
	.badge-destructive { background: hsl(var(--destructive)); color: hsl(var(--destructive-foreground)); }
	.badge-success { background: hsl(var(--success)); color: hsl(var(--success-foreground)); }
	.badge-warning { background: hsl(var(--warning)); color: hsl(var(--warning-foreground)); }
	.badge-outline { border-color: hsl(var(--border)); color: hsl(var(--foreground)); }
</style>
