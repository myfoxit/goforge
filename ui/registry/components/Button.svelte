<script lang="ts">
	import { cn } from "./utils";
	import type { Snippet } from "svelte";
	import type { HTMLButtonAttributes } from "svelte/elements";

	type Variant = "default" | "secondary" | "destructive" | "outline" | "ghost" | "link";
	type Size = "sm" | "md" | "lg" | "icon";

	interface Props extends HTMLButtonAttributes {
		variant?: Variant;
		size?: Size;
		loading?: boolean;
		href?: string;
		children?: Snippet;
	}

	let {
		variant = "default",
		size = "md",
		loading = false,
		href,
		class: klass = "",
		children,
		...rest
	}: Props = $props();
</script>

{#if href}
	<a {href} class={cn("btn", `btn-${variant}`, `btn-${size}`, klass)}>
		{@render children?.()}
	</a>
{:else}
	<button class={cn("btn", `btn-${variant}`, `btn-${size}`, klass)} disabled={loading || rest.disabled} {...rest}>
		{#if loading}<span class="btn-spinner" aria-hidden="true"></span>{/if}
		{@render children?.()}
	</button>
{/if}

<style>
	.btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 0.5rem;
		border-radius: calc(var(--radius) - 2px);
		font-size: 0.875rem;
		font-weight: 500;
		white-space: nowrap;
		cursor: pointer;
		border: 1px solid transparent;
		transition: background-color 0.15s, color 0.15s, border-color 0.15s, opacity 0.15s;
		user-select: none;
	}
	.btn:focus-visible {
		outline: 2px solid hsl(var(--ring));
		outline-offset: 2px;
	}
	.btn:disabled {
		opacity: 0.5;
		pointer-events: none;
	}
	.btn-sm { height: 2rem; padding: 0 0.75rem; font-size: 0.8125rem; }
	.btn-md { height: 2.25rem; padding: 0 1rem; }
	.btn-lg { height: 2.75rem; padding: 0 1.5rem; font-size: 0.9375rem; }
	.btn-icon { height: 2.25rem; width: 2.25rem; padding: 0; }

	.btn-default { background: hsl(var(--primary)); color: hsl(var(--primary-foreground)); }
	.btn-default:hover { background: hsl(var(--primary) / 0.9); }
	.btn-secondary { background: hsl(var(--secondary)); color: hsl(var(--secondary-foreground)); }
	.btn-secondary:hover { background: hsl(var(--secondary) / 0.8); }
	.btn-destructive { background: hsl(var(--destructive)); color: hsl(var(--destructive-foreground)); }
	.btn-destructive:hover { background: hsl(var(--destructive) / 0.9); }
	.btn-outline { border-color: hsl(var(--border)); background: hsl(var(--background)); color: hsl(var(--foreground)); }
	.btn-outline:hover { background: hsl(var(--accent)); color: hsl(var(--accent-foreground)); }
	.btn-ghost { background: transparent; color: hsl(var(--foreground)); }
	.btn-ghost:hover { background: hsl(var(--accent)); color: hsl(var(--accent-foreground)); }
	.btn-link { background: transparent; color: hsl(var(--primary)); text-decoration: underline; text-underline-offset: 4px; }

	.btn-spinner {
		width: 0.875rem;
		height: 0.875rem;
		border: 2px solid currentColor;
		border-right-color: transparent;
		border-radius: 50%;
		animation: btn-spin 0.6s linear infinite;
	}
	@keyframes btn-spin { to { transform: rotate(360deg); } }
</style>
