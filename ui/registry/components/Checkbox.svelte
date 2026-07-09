<script lang="ts">
	import { cn } from "./utils";
	interface Props {
		checked?: boolean;
		disabled?: boolean;
		class?: string;
		"aria-label"?: string;
	}
	let { checked = $bindable(false), disabled = false, class: klass = "", ...rest }: Props = $props();
</script>

<button
	type="button"
	role="checkbox"
	aria-checked={checked}
	{disabled}
	class={cn("checkbox", checked && "checkbox-on", klass)}
	onclick={() => (checked = !checked)}
	{...rest}
>
	{#if checked}<span class="checkbox-mark" aria-hidden="true">✓</span>{/if}
</button>

<style>
	.checkbox {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 1.1rem;
		height: 1.1rem;
		border-radius: calc(var(--radius) - 4px);
		border: 1px solid hsl(var(--input));
		background: hsl(var(--background));
		cursor: pointer;
		padding: 0;
	}
	.checkbox-on { background: hsl(var(--primary)); border-color: hsl(var(--primary)); }
	.checkbox:focus-visible { outline: 2px solid hsl(var(--ring)); outline-offset: 2px; }
	.checkbox:disabled { opacity: 0.5; cursor: not-allowed; }
	.checkbox-mark { color: hsl(var(--primary-foreground)); font-size: 0.7rem; line-height: 1; }
</style>
