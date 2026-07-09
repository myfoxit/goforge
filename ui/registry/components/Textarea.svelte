<script lang="ts">
	import { cn } from "./utils";
	import type { HTMLTextareaAttributes } from "svelte/elements";
	interface Props extends HTMLTextareaAttributes {
		value?: string;
		invalid?: boolean;
	}
	let { value = $bindable(""), invalid = false, class: klass = "", ...rest }: Props = $props();
</script>

<textarea class={cn("textarea", invalid && "textarea-invalid", klass)} bind:value {...rest}></textarea>

<style>
	.textarea {
		display: block;
		width: 100%;
		min-height: 5rem;
		border-radius: calc(var(--radius) - 2px);
		border: 1px solid hsl(var(--input));
		background: hsl(var(--background));
		color: hsl(var(--foreground));
		padding: 0.5rem 0.75rem;
		font-size: 0.875rem;
		font-family: inherit;
		resize: vertical;
	}
	.textarea:focus-visible {
		outline: none;
		border-color: hsl(var(--ring));
		box-shadow: 0 0 0 3px hsl(var(--ring) / 0.15);
	}
	.textarea-invalid { border-color: hsl(var(--destructive)); }
</style>
