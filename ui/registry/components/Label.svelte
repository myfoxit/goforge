<script lang="ts">
	import { cn } from "./utils";
	import type { Snippet } from "svelte";
	interface Props {
		for?: string;
		class?: string;
		children?: Snippet;
	}
	let { for: htmlFor, class: klass = "", children }: Props = $props();
</script>

<label for={htmlFor} class={cn("label", klass)}>{@render children?.()}</label>

<style>
	.label {
		font-size: 0.875rem;
		font-weight: 500;
		line-height: 1;
		color: hsl(var(--foreground));
	}
</style>
