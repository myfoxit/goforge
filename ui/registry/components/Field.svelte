<script lang="ts">
	import Label from "./Label.svelte";
	import type { Snippet } from "svelte";
	interface Props {
		label?: string;
		for?: string;
		error?: string;
		hint?: string;
		required?: boolean;
		children?: Snippet;
	}
	let { label, for: htmlFor, error, hint, required = false, children }: Props = $props();
</script>

<div class="field">
	{#if label}
		<Label for={htmlFor}>{label}{#if required}<span class="field-req">*</span>{/if}</Label>
	{/if}
	{@render children?.()}
	{#if error}<p class="field-error">{error}</p>{:else if hint}<p class="field-hint">{hint}</p>{/if}
</div>

<style>
	.field { display: flex; flex-direction: column; gap: 0.4rem; }
	.field-req { color: hsl(var(--destructive)); margin-left: 0.15rem; }
	.field-error { font-size: 0.8125rem; color: hsl(var(--destructive)); margin: 0; }
	.field-hint { font-size: 0.8125rem; color: hsl(var(--muted-foreground)); margin: 0; }
</style>
