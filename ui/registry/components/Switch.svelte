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
	role="switch"
	aria-checked={checked}
	{disabled}
	class={cn("switch", checked && "switch-on", klass)}
	onclick={() => (checked = !checked)}
	{...rest}
>
	<span class="switch-thumb"></span>
</button>

<style>
	.switch {
		display: inline-flex;
		align-items: center;
		width: 2.5rem;
		height: 1.4rem;
		border-radius: 9999px;
		border: 0;
		background: hsl(var(--input));
		cursor: pointer;
		padding: 2px;
		transition: background 0.15s;
	}
	.switch-on { background: hsl(var(--primary)); }
	.switch:disabled { opacity: 0.5; cursor: not-allowed; }
	.switch:focus-visible { outline: 2px solid hsl(var(--ring)); outline-offset: 2px; }
	.switch-thumb {
		width: 1rem;
		height: 1rem;
		border-radius: 50%;
		background: hsl(var(--background));
		box-shadow: 0 1px 2px hsl(0 0% 0% / 0.2);
		transition: transform 0.15s;
	}
	.switch-on .switch-thumb { transform: translateX(1.1rem); }
</style>
