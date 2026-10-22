/**
 * True when url is plain http on a host other than localhost: browsers only
 * offer passkeys on https (or localhost), so sign-in would not work there.
 */
export function passkeysNeedHttps(url: string): boolean {
	let u: URL;
	try {
		u = new URL(url.trim());
	} catch {
		return false;
	}
	if (u.protocol !== 'http:') return false;
	const h = u.hostname.toLowerCase();
	const local = h === 'localhost' || h.endsWith('.localhost') || h === '127.0.0.1' || h === '[::1]';
	return !local;
}
