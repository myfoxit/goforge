<script lang="ts">
	import { cn } from "./utils";
	interface Props { size?: number; class?: string; }
	let { size = 20, class: klass = "" }: Props = $props();
</script>

<span class={cn("spinner", klass)} style:width="{size}px" style:height="{size}px" role="status" aria-label="Loading"></span>

<style>
	.spinner {
		display: inline-block;
		border: 2px solid hsl(var(--muted-foreground) / 0.3);
		border-top-color: hsl(var(--foreground));
		border-radius: 50%;
		animation: spin 0.6s linear infinite;
	}
	@keyframes spin { to { transform: rotate(360deg); } }
</style>
