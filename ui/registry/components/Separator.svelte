<script lang="ts">
	import { cn } from "./utils";
	interface Props { orientation?: "horizontal" | "vertical"; class?: string; }
	let { orientation = "horizontal", class: klass = "" }: Props = $props();
</script>

<div class={cn("separator", orientation, klass)} role="separator" aria-orientation={orientation}></div>

<style>
	.separator { background: hsl(var(--border)); }
	.horizontal { height: 1px; width: 100%; }
	.vertical { width: 1px; align-self: stretch; }
</style>
