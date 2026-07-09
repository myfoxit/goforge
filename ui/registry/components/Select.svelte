<script lang="ts">
	import { cn } from "./utils";
	import type { HTMLSelectAttributes } from "svelte/elements";

	interface Option {
		value: string;
		label: string;
	}
	interface Props extends HTMLSelectAttributes {
		value?: string;
		options?: Option[];
		placeholder?: string;
	}
	let {
		value = $bindable(""),
		options = [],
		placeholder,
		class: klass = "",
		...rest
	}: Props = $props();
</script>

<div class={cn("select-wrap", klass)}>
	<select class="select" bind:value {...rest}>
		{#if placeholder}<option value="" disabled>{placeholder}</option>{/if}
		{#each options as opt}
			<option value={opt.value}>{opt.label}</option>
		{/each}
	</select>
	<span class="select-arrow" aria-hidden="true">▾</span>
</div>

<style>
	.select-wrap { position: relative; display: block; }
	.select {
		appearance: none;
		width: 100%;
		height: 2.25rem;
		border-radius: calc(var(--radius) - 2px);
		border: 1px solid hsl(var(--input));
		background: hsl(var(--background));
		color: hsl(var(--foreground));
		padding: 0 2rem 0 0.75rem;
		font-size: 0.875rem;
		cursor: pointer;
	}
	.select:focus-visible {
		outline: none;
		border-color: hsl(var(--ring));
		box-shadow: 0 0 0 3px hsl(var(--ring) / 0.15);
	}
	.select-arrow {
		position: absolute;
		right: 0.7rem;
		top: 50%;
		transform: translateY(-50%);
		pointer-events: none;
		color: hsl(var(--muted-foreground));
		font-size: 0.75rem;
	}
</style>
