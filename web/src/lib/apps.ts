export interface App {
	id: string;
	name: string;
	url: string;
	color: string;
	/** data: URL or null; only ever rendered through <img src>. */
	icon: string | null;
	protocol: string;
	sort: number;
	created_at: string;
}

/** Lowercase and strip diacritics so "Pröxmox" and "proxmox" compare equal. */
export function fold(s: string): string {
	return s.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase();
}

export function hostOf(url: string): string {
	try {
		return new URL(url).hostname;
	} catch {
		return '';
	}
}

/** Apps whose name or URL host contains query (case- and diacritic-insensitive). */
export function filterApps(apps: App[], query: string): App[] {
	const q = fold(query.trim());
	if (!q) return apps;
	return apps.filter((a) => fold(a.name).includes(q) || fold(hostOf(a.url)).includes(q));
}

/** url when it is an absolute http(s) URL, else null — never link javascript: or data:. */
export function safeHref(url: string): string | null {
	try {
		const { protocol } = new URL(url);
		return protocol === 'http:' || protocol === 'https:' ? url : null;
	} catch {
		return null;
	}
}
