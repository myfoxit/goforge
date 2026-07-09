<script lang="ts">
	import { cn } from "./utils";
	import type { Snippet } from "svelte";

	interface Tab {
		id: string;
		label: string;
	}
	interface Props {
		tabs: Tab[];
		active?: string;
		class?: string;
		children?: Snippet<[string]>;
	}
	let { tabs, active = $bindable(tabs[0]?.id ?? ""), class: klass = "", children }: Props = $props();
</script>

<div class={cn("tabs", klass)}>
	<div class="tabs-list" role="tablist">
		{#each tabs as tab}
			<button
				role="tab"
				aria-selected={active === tab.id}
				class:active={active === tab.id}
				class="tabs-trigger"
				onclick={() => (active = tab.id)}
			>
				{tab.label}
			</button>
		{/each}
	</div>
	<div class="tabs-content" role="tabpanel">
		{@render children?.(active)}
	</div>
</div>

<style>
	.tabs-list {
		display: inline-flex;
		gap: 0.25rem;
		padding: 0.25rem;
		background: hsl(var(--muted));
		border-radius: var(--radius);
	}
	.tabs-trigger {
		border: 0;
		background: transparent;
		color: hsl(var(--muted-foreground));
		padding: 0.35rem 0.85rem;
		font-size: 0.875rem;
		font-weight: 500;
		border-radius: calc(var(--radius) - 2px);
		cursor: pointer;
		transition: background 0.15s, color 0.15s;
	}
	.tabs-trigger.active {
		background: hsl(var(--background));
		color: hsl(var(--foreground));
		box-shadow: 0 1px 2px hsl(0 0% 0% / 0.1);
	}
	.tabs-content { margin-top: 1rem; }
</style>
