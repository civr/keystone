/** The app shell's left menu: "Mijn apps", then the Instellingen sections. */
export interface NavItem {
	href: string;
	label: string;
	/** SVG path data on a 20×20 grid, stroked in currentColor. */
	icon: string;
}
export interface NavGroup {
	/** Group heading; null for the ungrouped top items. */
	title: string | null;
	items: NavItem[];
}

export const NAV: NavGroup[] = [
	{
		title: null,
		items: [
			{
				href: '/',
				label: 'Mijn apps',
				icon: 'M3.5 3.5h5v5h-5zM11.5 3.5h5v5h-5zM3.5 11.5h5v5h-5zM11.5 11.5h5v5h-5z'
			},
			{
				href: '/kluis',
				label: 'Kluis',
				icon: 'M3 4h14v11.5H3zM10 7a2.75 2.75 0 1 0 0 5.5A2.75 2.75 0 0 0 10 7zM5.5 15.5V17M14.5 15.5V17'
			}
		]
	},
	{
		title: 'Instellingen',
		items: [
			{
				href: '/instellingen',
				label: 'Algemeen',
				icon: 'M10 2.8v2M10 15.2v2M2.8 10h2M15.2 10h2M4.9 4.9l1.4 1.4M13.7 13.7l1.4 1.4M4.9 15.1l1.4-1.4M13.7 6.3l1.4-1.4M10 6.8a3.2 3.2 0 1 0 0 6.4 3.2 3.2 0 0 0 0-6.4z'
			},
			{
				href: '/instellingen/huisstijl',
				label: 'Huisstijl',
				icon: 'M10 3a7 7 0 1 0 0 14c.9 0 1.5-.6 1.5-1.4 0-1.1-1-1.4-1-2.5 0-.8.7-1.5 1.5-1.5h2a3 3 0 0 0 3-3C17 5.7 13.9 3 10 3zM6.5 10h.01M8 6.8h.01M12 6.8h.01'
			},
			{
				href: '/instellingen/ad',
				label: 'Active Directory',
				icon: 'M3 4.5h14v5H3zM3 10.5h14v5H3zM6 7h.01M6 13h.01'
			},
			{
				href: '/instellingen/apps',
				label: 'Apps',
				icon: 'M4 4h12v12H4zM4 8.5h12M8.5 8.5V16'
			},
			{
				href: '/instellingen/beveiliging',
				label: 'Beveiliging',
				icon: 'M10 2.5 16 5v4.6c0 3.7-2.6 6.6-6 7.9-3.4-1.3-6-4.2-6-7.9V5z'
			},
			{
				href: '/instellingen/apparaten',
				label: 'Apparaten',
				icon: 'M2.5 4.5h11v8h-11zM6 15.5h4M15 7.5h3v8.5h-3z'
			},
			{
				href: '/instellingen/activiteit',
				label: 'Activiteit',
				icon: 'M10 3.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13zM10 6.5V10l2.5 1.5'
			}
		]
	}
];

const trim = (p: string) => p.replace(/\/+$/, '') || '/';

/** The menu item for href is the current page (exact match; a trailing slash is ignored). */
export function isActive(pathname: string, href: string): boolean {
	return trim(pathname) === trim(href);
}

/** Up to two initials for the avatar, e.g. "Thijs Willems" → "TW". */
export function initials(name: string): string {
	return (
		name
			.split(/\s+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((w) => Array.from(w)[0])
			.join('')
			.toUpperCase() || '?'
	);
}

/** Where the top-bar search goes from a page other than home. */
export function searchHref(query: string): string {
	const q = query.trim();
	return q ? `/?q=${encodeURIComponent(q)}` : '/';
}

/**
 * Where the sign-in page may send the browser after logging in: only back to
 * keystone-core's own sign-in continuations (SAML, OIDC, gate). Anything else,
 * including other hosts and dot-segment tricks, becomes "/".
 */
export function safeNext(raw: string | null): string {
	if (!raw || !raw.startsWith('/') || raw.startsWith('//') || raw.includes('\\')) return '/';
	let u: URL;
	try {
		u = new URL(raw, 'https://keystone.invalid');
	} catch {
		return '/';
	}
	const path = raw.split(/[?#]/, 1)[0];
	if (u.origin !== 'https://keystone.invalid' || u.pathname !== path) return '/';
	const ok = path === '/oidc/authorize' || path === '/api/gate/start' || /^\/saml\/[a-z]/.test(path);
	return ok ? raw : '/';
}
