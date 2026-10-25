/**
 * The two portals' menus. The user portal (My apps, Vault, Account) is for
 * everyone; the admin portal (/admin) shows each section only to accounts
 * holding one of its permissions (package rbac on the server, which checks
 * again on every request).
 */
export interface NavItem {
	href: string;
	label: string;
	/** SVG path data on a 20×20 grid, stroked in currentColor. */
	icon: string;
	/** Colour of the item's icon tile. */
	tint: string;
	/** Shown when the account holds any of these permissions ("*" holds all). */
	perms?: string[];
	/** Only the owner sees it. */
	owner?: boolean;
}
export interface NavGroup {
	/** Group heading; null for the ungrouped top items. */
	title: string | null;
	items: NavItem[];
}

const I = {
	apps: 'M3.5 3.5h5v5h-5zM11.5 3.5h5v5h-5zM3.5 11.5h5v5h-5zM11.5 11.5h5v5h-5z',
	vault: 'M3 4h14v11.5H3zM10 7a2.75 2.75 0 1 0 0 5.5A2.75 2.75 0 0 0 10 7zM5.5 15.5V17M14.5 15.5V17',
	person: 'M10 3.5a3.25 3.25 0 1 0 0 6.5 3.25 3.25 0 0 0 0-6.5zM3.75 16.5c.8-3 3.3-4.6 6.25-4.6s5.45 1.6 6.25 4.6',
	shield: 'M10 2.5 16 5v4.6c0 3.7-2.6 6.6-6 7.9-3.4-1.3-6-4.2-6-7.9V5z',
	devices: 'M2.5 4.5h11v8h-11zM6 15.5h4M15 7.5h3v8.5h-3z',
	key: 'M7.5 3.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7zM10.3 9.3l6.2 6.2M14 13l1.8-1.8M12.4 11.4l1.6-1.6',
	sliders: 'M4 5h12M4 10h12M4 15h12M7 3.5v3M13 8.5v3M9 13.5v3',
	dash: 'M3.5 3.5h5.5v7H3.5zM11 3.5h5.5v4H11zM11 9.5h5.5v7H11zM3.5 12.5h5.5v4H3.5z',
	users: 'M7.5 4a2.75 2.75 0 1 0 0 5.5A2.75 2.75 0 0 0 7.5 4zM2.5 16c.6-2.6 2.6-4 5-4s4.4 1.4 5 4M13.5 4.5a2.5 2.5 0 0 1 0 5M14.5 12.2c1.6.4 2.7 1.7 3 3.8',
	groups: 'M4 6.5a2 2 0 1 0 4 0 2 2 0 0 0-4 0zM12 6.5a2 2 0 1 0 4 0 2 2 0 0 0-4 0zM2.5 15c.4-2 1.8-3.2 3.5-3.2S9.1 13 9.5 15M10.5 15c.4-2 1.8-3.2 3.5-3.2s3.1 1.2 3.5 3.2',
	badge: 'M10 2.8l2 1.5 2.5-.2.8 2.4 2 1.5-.8 2.4.8 2.4-2 1.5-.8 2.4-2.5-.2-2 1.5-2-1.5-2.5.2-.8-2.4-2-1.5.8-2.4-.8-2.4 2-1.5.8-2.4 2.5.2zM7.5 10l1.7 1.7 3.3-3.4',
	window: 'M3 4h14v12H3zM3 7.5h14M5.5 5.8h.01M7.5 5.8h.01',
	plug: 'M7 3v4M13 3v4M5.5 7h9v2.5a4.5 4.5 0 0 1-9 0zM10 14v3',
	lock: 'M6 9V7a4 4 0 0 1 8 0v2M5 9h10v7.5H5z',
	directory: 'M3 4.5h14v5H3zM3 10.5h14v5H3zM6 7h.01M6 13h.01',
	globe: 'M10 3a7 7 0 1 0 0 14 7 7 0 0 0 0-14zM3 10h14M10 3c2 2 2.8 4.4 2.8 7S12 15 10 17c-2-2-2.8-4.4-2.8-7S8 5 10 3z',
	clock: 'M10 3.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13zM10 6.5V10l2.5 1.5',
	gear: 'M10 2.8v2M10 15.2v2M2.8 10h2M15.2 10h2M4.9 4.9l1.4 1.4M13.7 13.7l1.4 1.4M4.9 15.1l1.4-1.4M13.7 6.3l1.4-1.4M10 6.8a3.2 3.2 0 1 0 0 6.4 3.2 3.2 0 0 0 0-6.4z',
	palette: 'M10 3a7 7 0 1 0 0 14c.9 0 1.5-.6 1.5-1.4 0-1.1-1-1.4-1-2.5 0-.8.7-1.5 1.5-1.5h2a3 3 0 0 0 3-3C17 5.7 13.9 3 10 3zM6.5 10h.01M8 6.8h.01M12 6.8h.01'
};

/** The user portal: for everyone. */
export const USER_NAV: NavGroup[] = [
	{
		title: null,
		items: [
			{ href: '/', label: 'My apps', icon: I.apps, tint: '#007aff' },
			{ href: '/vault', label: 'Vault', icon: I.vault, tint: '#34c759' }
		]
	},
	{
		title: 'Account',
		items: [
			{ href: '/account', label: 'Profile', icon: I.person, tint: '#5ac8fa' },
			{ href: '/account/security', label: 'Sign-in & devices', icon: I.shield, tint: '#30b0c7' },
			{ href: '/account/access', label: 'My access', icon: I.key, tint: '#ff9500' },
			{ href: '/account/preferences', label: 'Preferences', icon: I.sliders, tint: '#8e8e93' }
		]
	}
];

/** The admin portal (/admin). */
export const ADMIN_NAV: NavGroup[] = [
	{ title: null, items: [{ href: '/admin', label: 'Dashboard', icon: I.dash, tint: '#007aff', perms: [] }] },
	{
		title: 'Directory',
		items: [
			{ href: '/admin/users', label: 'Users', icon: I.users, tint: '#ff9500', perms: ['users.read', 'users.write'] },
			{ href: '/admin/groups', label: 'Groups', icon: I.groups, tint: '#ff9f0a', perms: ['users.read', 'groups.write'] },
			{ href: '/admin/roles', label: 'Roles', icon: I.badge, tint: '#af52de', perms: ['users.read', 'roles.write'] }
		]
	},
	{
		title: 'Applications',
		items: [
			{ href: '/admin/apps', label: 'Applications', icon: I.window, tint: '#ff2d55', perms: ['apps.read', 'apps.write'] },
			{ href: '/admin/protocols', label: 'Protocols', icon: I.plug, tint: '#5856d6', perms: ['security.write'] }
		]
	},
	{
		title: 'Authentication',
		items: [
			{ href: '/admin/authentication', label: 'Sign-in & MFA', icon: I.lock, tint: '#30b0c7', perms: ['security.write', 'auth.write'] },
			{ href: '/admin/identity', label: 'Identity providers', icon: I.directory, tint: '#636366', owner: true },
			{ href: '/admin/sessions', label: 'Sessions', icon: I.devices, tint: '#5ac8fa', perms: ['sessions.manage'] }
		]
	},
	{
		title: 'Security',
		items: [
			{ href: '/admin/vault', label: 'Vault & secrets', icon: I.vault, tint: '#34c759', perms: ['security.write', 'vault.write'] },
			{ href: '/admin/network', label: 'Network', icon: I.globe, tint: '#0a84ff', perms: ['security.write'] },
			{ href: '/admin/keys', label: 'Keys & recovery', icon: I.key, tint: '#ffcc00', perms: ['security.write'] },
			{ href: '/admin/audit', label: 'Audit log', icon: I.clock, tint: '#ff3b30', perms: ['audit.read'] }
		]
	},
	{
		title: 'System',
		items: [
			{ href: '/admin/system', label: 'General', icon: I.gear, tint: '#8e8e93', perms: ['system.write'] },
			{ href: '/admin/branding', label: 'Branding', icon: I.palette, tint: '#bf5af2', perms: ['branding.write'] }
		]
	}
];

/** What one account may see in a menu. */
export interface Access {
	permissions: string[];
	owner: boolean;
}

/** Whether access opens item. */
export function canSee(item: NavItem, a: Access): boolean {
	if (item.owner) return a.owner;
	if (!item.perms) return true;
	if (a.permissions.includes('*')) return true;
	if (item.perms.length === 0) return a.permissions.length > 0;
	return item.perms.some((p) => a.permissions.includes(p));
}

/** The menu filtered for access; empty groups disappear. */
export function navFor(nav: NavGroup[], a: Access): NavGroup[] {
	return nav.map((g) => ({ ...g, items: g.items.filter((i) => canSee(i, a)) })).filter((g) => g.items.length > 0);
}

/** Old paths and where they live now. */
export const MOVED: Record<string, string> = {
	'/kluis': '/vault',
	'/instellingen': '/admin/system',
	'/instellingen/huisstijl': '/admin/branding',
	'/instellingen/ad': '/admin/identity',
	'/instellingen/apps': '/admin/apps',
	'/instellingen/beveiliging': '/admin/authentication',
	'/instellingen/apparaten': '/account/security',
	'/instellingen/activiteit': '/admin/audit',
	'/settings': '/admin',
	'/settings/account': '/account',
	'/settings/devices': '/account/security',
	'/settings/users': '/admin/users',
	'/settings/apps': '/admin/apps',
	'/settings/branding': '/admin/branding',
	'/settings/security': '/admin/authentication',
	'/settings/ad': '/admin/identity',
	'/settings/activity': '/admin/audit'
};

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
