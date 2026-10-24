import { describe, expect, it } from 'vitest';
import { NAV, initials, isActive, navFor, searchHref, safeNext } from './nav';

describe('isActive', () => {
	it.each([
		['/', '/', true],
		['/settings', '/settings', true],
		['/settings/', '/settings', true],
		['/settings/ad', '/settings', false],
		['/settings/ad/', '/settings/ad', true],
		['/settings', '/', false],
		['/login', '/', false]
	])('%s on %s → %s', (path, href, want) => {
		expect(isActive(path, href)).toBe(want);
	});
});

describe('NAV', () => {
	it('has My apps and Vault first, then the Settings group in order', () => {
		expect(NAV.map((g) => g.title)).toEqual([null, 'Settings']);
		expect(NAV[0].items.map((i) => [i.label, i.href])).toEqual([
			['My apps', '/'],
			['Vault', '/vault']
		]);
		expect(NAV[1].items.map((i) => i.href)).toEqual([
			'/settings',
			'/settings/account',
			'/settings/devices',
			'/settings/users',
			'/settings/apps',
			'/settings/branding',
			'/settings/security',
			'/settings/ad',
			'/settings/activity'
		]);
	});
	it('shows members only their own sections', () => {
		expect(navFor(false)[1].items.map((i) => i.label)).toEqual(['Account', 'Devices']);
		expect(navFor(true)[1].items).toHaveLength(9);
	});
});

describe('initials', () => {
	it.each([
		['Thijs Willems', 'TW'],
		['thijs', 'T'],
		['  Anna  van  Dijk ', 'AV'],
		['émile zola', 'ÉZ'],
		['', '?'],
		['   ', '?']
	])('%j → %s', (name, want) => {
		expect(initials(name)).toBe(want);
	});
});

describe('searchHref', () => {
	it('carries a non-empty query to the home page', () => {
		expect(searchHref(' jelly fin ')).toBe('/?q=jelly%20fin');
	});
	it('is the plain home page for a blank query', () => {
		expect(searchHref('   ')).toBe('/');
	});
});

describe('safeNext', () => {
	it('allows the server sign-in continuations', () => {
		expect(safeNext('/oidc/authorize?client_id=x&state=y')).toBe('/oidc/authorize?client_id=x&state=y');
		expect(safeNext('/saml/sso?resume=abc')).toBe('/saml/sso?resume=abc');
		expect(safeNext('/api/gate/start?rd=https%3A%2F%2Fha.home.lan%2F')).toBe('/api/gate/start?rd=https%3A%2F%2Fha.home.lan%2F');
	});
	it('rejects everything else', () => {
		for (const bad of [null, '', '//evil.com/oidc/authorize', 'https://evil.com', '/settings', '/\\evil.com', '/oidc/authorizex', '/saml/../instellingen', ' /saml/sso']) {
			expect(safeNext(bad)).toBe('/');
		}
	});
});
