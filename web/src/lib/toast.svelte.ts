export interface ToastItem {
	id: number;
	text: string;
	kind: 'info' | 'error';
}

let next = 1;
export const toasts: ToastItem[] = $state([]);

/** Shows a short, non-blocking message; errors linger a little longer. */
export function toast(text: string, kind: ToastItem['kind'] = 'info'): void {
	const id = next++;
	toasts.push({ id, text, kind });
	if (toasts.length > 3) toasts.shift();
	setTimeout(() => dismiss(id), kind === 'error' ? 6000 : 3500);
}

export function dismiss(id: number): void {
	const i = toasts.findIndex((t) => t.id === id);
	if (i >= 0) toasts.splice(i, 1);
}
