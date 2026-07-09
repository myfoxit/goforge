<script lang="ts">
	import { cn } from "./utils";
	interface Props { src?: string; alt?: string; fallback?: string; size?: number; class?: string; }
	let { src, alt = "", fallback = "", size = 36, class: klass = "" }: Props = $props();
	let failed = $state(false);
	const initials = $derived(
		fallback || alt.split(" ").map((s) => s[0]).slice(0, 2).join("").toUpperCase()
	);
</script>

<span class={cn("avatar", klass)} style:width="{size}px" style:height="{size}px" style:font-size="{size / 2.6}px">
	{#if src && !failed}
		<img {src} {alt} onerror={() => (failed = true)} />
	{:else}
		<span class="avatar-fallback">{initials}</span>
	{/if}
</span>

<style>
	.avatar {
		display: inline-flex; align-items: center; justify-content: center;
		border-radius: 50%; overflow: hidden; background: hsl(var(--muted));
		color: hsl(var(--muted-foreground)); font-weight: 500; flex-shrink: 0;
	}
	.avatar img { width: 100%; height: 100%; object-fit: cover; }
</style>
