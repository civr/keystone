/** The part of navigator.clipboard used here (injectable for tests). */
export interface ClipboardLike {
	writeText(text: string): Promise<void>;
	readText?(): Promise<string>;
	write?(items: ClipboardItem[]): Promise<void>;
}

/**
 * Copies a password to the clipboard and wipes it after `seconds` (spec §5),
 * unless something else was copied meanwhile. The text may still be on its
 * way: with ClipboardItem the write starts inside the click, as Safari
 * requires, and resolves when the password arrives.
 */
export async function copyThenClear(text: Promise<string>, seconds: number, clipboard: ClipboardLike = navigator.clipboard): Promise<void> {
	if (clipboard.write && typeof ClipboardItem !== 'undefined') {
		const blob = text.then((t) => new Blob([t], { type: 'text/plain' }));
		await clipboard.write([new ClipboardItem({ 'text/plain': blob })]);
	} else {
		await clipboard.writeText(await text);
	}
	const copied = await text;
	setTimeout(async () => {
		try {
			if (clipboard.readText && (await clipboard.readText()) !== copied) return;
		} catch {
			/* reading not permitted: clear anyway */
		}
		await clipboard.writeText('').catch(() => {});
	}, seconds * 1000);
}
