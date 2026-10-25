import { describe, expect, it } from 'vitest';
import { ADMIN_NAV, USER_NAV, initials, isActive, navFor, searchHref, safeNext } from './nav';

describe('isActive', () => {
	it.each([
		['/', '/', true],
		['/admin', '/admin', true],
		['/admin/', '/admin', true],
		['/admin/users', '/admin', false],
		['/admin/users/', '/admin/users', true],
		['/admin', '/', false],
		['/login', '/', false]
	])('%s on %s → %s', (path, href, want) => {
		expect(isActive(path, href)).toBe(want);
	});
});

describe('menus', () => {
	const member = { permissions: [], owner: false };
	const auditor = { permissions: ['users.read', 'apps.read', 'audit.read'], owner: false };
	const owner = { permissions: ['*'], owner: true };
	it('give everyone the user portal', () => {
		expect(navFor(USER_NAV, member).flatMap((g) => g.items.map((i) => i.href))).toEqual([
			'/',
			'/vault',
			'/account',
			'/account/security',
			'/account/access',
			'/account/preferences'
		]);
	});
	it('show admin sections by permission', () => {
		expect(navFor(ADMIN_NAV, member)).toEqual([]);
		const hrefs = navFor(ADMIN_NAV, auditor).flatMap((g) => g.items.map((i) => i.href));
		expect(hrefs).toEqual(['/admin', '/admin/users', '/admin/groups', '/admin/roles', '/admin/apps', '/admin/audit']);
		expect(navFor(ADMIN_NAV, owner).flatMap((g) => g.items)).toHaveLength(ADMIN_NAV.flatMap((g) => g.items).length);
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
