import { describe, expect, it } from 'vitest';
import { filterApps, safeHref, type App } from './apps';

const app = (name: string, url: string): App => ({
	id: name,
	name,
	url,
	color: '#007aff',
	icon: null,
	protocol: 'link',
	config: {},
	sort: 0,
	created_at: '2026-10-20T00:00:00Z'
});

const apps = [
	app('Proxmox', 'https://pve.home.lan:8006/'),
	app('Jellyfin', 'https://media.home.lan'),
	app('Nextcloud', 'https://cloud.home.lan'),
	app('Café', 'https://koffie.home.lan')
];
const names = (q: string) => filterApps(apps, q).map((a) => a.name);

describe('filterApps', () => {
	it('returns all apps for an empty or blank query', () => {
		expect(names('')).toEqual(['Proxmox', 'Jellyfin', 'Nextcloud', 'Café']);
		expect(names('   ')).toHaveLength(4);
	});
	it('matches name case-insensitively', () => {
		expect(names('jelly')).toEqual(['Jellyfin']);
		expect(names('JELLY')).toEqual(['Jellyfin']);
	});
	it('matches the URL host', () => {
		expect(names('media')).toEqual(['Jellyfin']);
		expect(names('pve.home')).toEqual(['Proxmox']);
	});
	it('ignores diacritics on both sides', () => {
		expect(names('pröxmox')).toEqual(['Proxmox']);
		expect(names('cafe')).toEqual(['Café']);
	});
	it('does not match on scheme or path', () => {
		expect(names('https')).toEqual([]);
		expect(names('8006')).toEqual([]);
	});
	it('tolerates malformed URLs', () => {
		expect(filterApps([app('Kapot', 'not a url')], 'kap')).toHaveLength(1);
	});
});

describe('safeHref', () => {
	it('allows http and https URLs', () => {
		expect(safeHref('https://pve.home.lan:8006/')).toBe('https://pve.home.lan:8006/');
		expect(safeHref('http://printer.lan')).toBe('http://printer.lan');
	});
	it('rejects other schemes and garbage', () => {
		expect(safeHref('javascript:alert(1)')).toBeNull();
		expect(safeHref(' JavaScript:alert(1)')).toBeNull();
		expect(safeHref('data:text/html,hi')).toBeNull();
		expect(safeHref('not a url')).toBeNull();
	});
});
