<script lang="ts">
	import "../app.css";
	import { onMount } from "svelte";
	import { Toaster } from "$ui";
	import { session } from "$lib/session.svelte";
	let { children } = $props();

	onMount(() => {
		// If we arrived from an OAuth callback, adopt the handed-back session.
		session.finishOAuth();
	});
</script>

{@render children()}
<Toaster />

<style>
	:global(.full) { width: 100%; }
</style>
