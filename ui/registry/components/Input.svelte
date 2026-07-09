<script lang="ts">
	import { cn } from "./utils";
	import type { HTMLInputAttributes } from "svelte/elements";

	interface Props extends HTMLInputAttributes {
		value?: string | number;
		invalid?: boolean;
	}
	let { value = $bindable(""), invalid = false, class: klass = "", ...rest }: Props = $props();
</script>

<input class={cn("input", invalid && "input-invalid", klass)} bind:value {...rest} />

<style>
	.input {
		display: flex;
		width: 100%;
		height: 2.25rem;
		border-radius: calc(var(--radius) - 2px);
		border: 1px solid hsl(var(--input));
		background: hsl(var(--background));
		color: hsl(var(--foreground));
		padding: 0 0.75rem;
		font-size: 0.875rem;
		transition: border-color 0.15s, box-shadow 0.15s;
	}
	.input::placeholder { color: hsl(var(--muted-foreground)); }
	.input:focus-visible {
		outline: none;
		border-color: hsl(var(--ring));
		box-shadow: 0 0 0 3px hsl(var(--ring) / 0.15);
	}
	.input:disabled { opacity: 0.5; cursor: not-allowed; }
	.input-invalid { border-color: hsl(var(--destructive)); }
</style>
