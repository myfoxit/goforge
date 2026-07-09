<script lang="ts">
	import { cn } from "./utils";
	import type { Snippet } from "svelte";

	interface Column {
		key: string;
		label: string;
		width?: string;
		align?: "left" | "right" | "center";
	}
	interface Props {
		columns: Column[];
		rows: Record<string, any>[];
		class?: string;
		empty?: string;
		onrowclick?: (row: Record<string, any>) => void;
		cell?: Snippet<[{ row: Record<string, any>; column: Column }]>;
	}
	let { columns, rows, class: klass = "", empty = "No records.", onrowclick, cell }: Props = $props();
</script>

<div class={cn("table-wrap", klass)}>
	<table class="table">
		<thead>
			<tr>
				{#each columns as col}
					<th style:width={col.width} style:text-align={col.align ?? "left"}>{col.label}</th>
				{/each}
			</tr>
		</thead>
		<tbody>
			{#if rows.length === 0}
				<tr><td class="table-empty" colspan={columns.length}>{empty}</td></tr>
			{:else}
				{#each rows as row}
					<tr class:clickable={!!onrowclick} onclick={() => onrowclick?.(row)}>
						{#each columns as col}
							<td style:text-align={col.align ?? "left"}>
								{#if cell}{@render cell({ row, column: col })}{:else}{row[col.key] ?? ""}{/if}
							</td>
						{/each}
					</tr>
				{/each}
			{/if}
		</tbody>
	</table>
</div>

<style>
	.table-wrap {
		width: 100%;
		overflow-x: auto;
		border: 1px solid hsl(var(--border));
		border-radius: var(--radius);
	}
	.table { width: 100%; border-collapse: collapse; font-size: 0.875rem; }
	.table th {
		text-align: left;
		padding: 0.625rem 1rem;
		font-weight: 500;
		color: hsl(var(--muted-foreground));
		border-bottom: 1px solid hsl(var(--border));
		background: hsl(var(--muted) / 0.4);
		white-space: nowrap;
	}
	.table td {
		padding: 0.625rem 1rem;
		border-bottom: 1px solid hsl(var(--border));
	}
	.table tbody tr:last-child td { border-bottom: 0; }
	.table tbody tr.clickable { cursor: pointer; }
	.table tbody tr.clickable:hover { background: hsl(var(--accent) / 0.5); }
	.table-empty { text-align: center; color: hsl(var(--muted-foreground)); padding: 2rem; }
</style>
