/** Helpers for editing app tiles in Instellingen › Apps (mirrors keystone-core's rules). */

/** Server limit for the whole icon data URL (apps.go maxIconLen). */
export const MAX_ICON = 64 * 1024;
export const ICON_TYPES = ['image/png', 'image/svg+xml', 'image/webp'];

/** A copy of list with the item at from moved to to (clamped). */
export function moveItem<T>(list: readonly T[], from: number, to: number): T[] {
	const out = list.slice();
	const [item] = out.splice(from, 1);
	out.splice(Math.max(0, Math.min(out.length, to)), 0, item);
	return out;
}

/**
 * Where the dragged row (index from) lands when the pointer is at y, given the
 * vertical midpoints of all rows as laid out when the drag began: past every
 * other row whose midpoint is above the pointer.
 */
export function targetIndex(mids: readonly number[], from: number, y: number): number {
	return mids.filter((m, i) => i !== from && m < y).length;
}

/** Error for an icon file (and its data URL once read), or null when acceptable. */
export function iconError(file: { type: string; size: number }, dataUrl?: string): string | null {
	if (!ICON_TYPES.includes(file.type)) return 'Choose a PNG, SVG or WebP image.';
	if (file.size > MAX_ICON || (dataUrl !== undefined && dataUrl.length > MAX_ICON))
		return 'The icon may be at most 64 kB. Use a smaller image, for example 180 × 180 px.';
	return null;
}

export interface AppForm {
	name: string;
	url: string;
	color: string;
}

/** First problem with the form as {field, message}, or null. Same rules as the API. */
export function appFormError(f: AppForm): { field: keyof AppForm; message: string } | null {
	const n = Array.from(f.name.trim()).length;
	if (n < 1 || n > 40) return { field: 'name', message: 'The name must be 1 to 40 characters.' };
	let ok = false;
	try {
		const u = new URL(f.url.trim());
		ok = (u.protocol === 'http:' || u.protocol === 'https:') && u.host !== '';
	} catch {
		/* not absolute */
	}
	if (!ok) return { field: 'url', message: 'The URL must start with http:// or https://.' };
	if (!/^#[0-9a-f]{6}$/i.test(f.color)) return { field: 'color', message: 'Choose a colour.' };
	return null;
}

/** "a, b\nc" → ["a", "b", "c"]: lists typed one per line (commas and spaces also split). */
export function parseList(text: string): string[] {
	return text.split(/[\s,]+/).filter(Boolean);
}
