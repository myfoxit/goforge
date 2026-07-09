// Toast store — a tiny reactive queue consumed by <Toaster>.
import { writable } from "svelte/store";

export type ToastVariant = "default" | "success" | "error" | "warning";

export interface Toast {
	id: number;
	title: string;
	description?: string;
	variant: ToastVariant;
	duration: number;
}

function createToaster() {
	const { subscribe, update } = writable<Toast[]>([]);
	let counter = 0;

	function push(
		title: string,
		opts: { description?: string; variant?: ToastVariant; duration?: number } = {},
	) {
		const id = ++counter;
		const toast: Toast = {
			id,
			title,
			description: opts.description,
			variant: opts.variant ?? "default",
			duration: opts.duration ?? 4000,
		};
		update((all) => [...all, toast]);
		if (toast.duration > 0) {
			setTimeout(() => dismiss(id), toast.duration);
		}
		return id;
	}

	function dismiss(id: number) {
		update((all) => all.filter((t) => t.id !== id));
	}

	return {
		subscribe,
		dismiss,
		toast: push,
		success: (title: string, description?: string) => push(title, { variant: "success", description }),
		error: (title: string, description?: string) => push(title, { variant: "error", description }),
		warning: (title: string, description?: string) => push(title, { variant: "warning", description }),
	};
}

export const toaster = createToaster();
