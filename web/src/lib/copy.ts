/**
 * Copies text from a click. navigator.clipboard exists only on https (and
 * localhost); on plain http the older execCommand route still works inside
 * the click itself.
 */
export async function copyText(text: string): Promise<void> {
	if (window.isSecureContext && navigator.clipboard) return navigator.clipboard.writeText(text);
	const area = document.createElement('textarea');
	area.value = text;
	area.setAttribute('readonly', '');
	area.style.position = 'fixed';
	area.style.opacity = '0';
	document.body.append(area);
	area.select();
	const ok = document.execCommand('copy');
	area.remove();
	if (!ok) throw new Error('copy failed');
}

/** Whether a password can be copied without a click of its own. */
export function canCopyDirectly(): boolean {
	return window.isSecureContext && !!navigator.clipboard;
}
