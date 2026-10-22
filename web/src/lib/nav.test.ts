import { describe, expect, it } from 'vitest';
import { NAV, initials, isActive, searchHref } from './nav';

describe('isActive', () => {
	it.each([
		['/', '/', true],
		['/instellingen', '/instellingen', true],
		['/instellingen/', '/instellingen', true],
		['/instellingen/ad', '/instellingen', false],
		['/instellingen/ad/', '/instellingen/ad', true],
		['/instellingen', '/', false],
		['/login', '/', false]
	])('%s on %s → %s', (path, href, want) => {
		expect(isActive(path, href)).toBe(want);
	});
});

describe('NAV', () => {
	it('has Mijn apps first, then the Instellingen group in order', () => {
		expect(NAV.map((g) => g.title)).toEqual([null, 'Instellingen']);
		expect(NAV[0].items.map((i) => [i.label, i.href])).toEqual([['Mijn apps', '/']]);
		expect(NAV[1].items.map((i) => [i.label, i.href])).toEqual([
			['Algemeen', '/instellingen'],
			['Active Directory', '/instellingen/ad'],
			['Apps', '/instellingen/apps'],
			['Beveiliging', '/instellingen/beveiliging'],
			['Apparaten', '/instellingen/apparaten'],
			['Activiteit', '/instellingen/activiteit']
		]);
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
