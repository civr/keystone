import { describe, expect, it } from 'vitest';
import { appFormError, iconError, MAX_ICON, moveItem, targetIndex, parseList } from './appEdit';

describe('moveItem', () => {
	it('moves an item and leaves the input untouched', () => {
		const a = ['a', 'b', 'c', 'd'];
		expect(moveItem(a, 0, 2)).toEqual(['b', 'c', 'a', 'd']);
		expect(moveItem(a, 3, 0)).toEqual(['d', 'a', 'b', 'c']);
		expect(moveItem(a, 1, 1)).toEqual(a);
		expect(a).toEqual(['a', 'b', 'c', 'd']);
	});
	it('clamps the target into range', () => {
		expect(moveItem(['a', 'b', 'c'], 0, 9)).toEqual(['b', 'c', 'a']);
		expect(moveItem(['a', 'b', 'c'], 2, -1)).toEqual(['c', 'a', 'b']);
	});
});

describe('targetIndex', () => {
	// Row midpoints at y = 25, 75, 125, 175 (rows 50 px tall).
	const mids = [25, 75, 125, 175];
	it('stays put near its own slot', () => {
		expect(targetIndex(mids, 1, 70)).toBe(1);
		expect(targetIndex(mids, 1, 95)).toBe(1);
	});
	it('passes a neighbour once the pointer crosses its midpoint', () => {
		expect(targetIndex(mids, 1, 130)).toBe(2);
		expect(targetIndex(mids, 1, 20)).toBe(0);
		expect(targetIndex(mids, 0, 500)).toBe(3);
		expect(targetIndex(mids, 3, -50)).toBe(0);
	});
});

describe('iconError', () => {
	it('accepts png, svg and webp up to 64 kB', () => {
		expect(iconError({ type: 'image/png', size: 1000 })).toBeNull();
		expect(iconError({ type: 'image/svg+xml', size: 1000 })).toBeNull();
		expect(iconError({ type: 'image/webp', size: MAX_ICON })).toBeNull();
	});
	it('rejects other types and large files', () => {
		expect(iconError({ type: 'image/jpeg', size: 1000 })).toMatch(/PNG, SVG or WebP/);
		expect(iconError({ type: 'text/html', size: 10 })).toMatch(/PNG, SVG or WebP/);
		expect(iconError({ type: 'image/png', size: MAX_ICON + 1 })).toMatch(/64 kB/);
	});
	it('checks the encoded data URL length like the server', () => {
		expect(iconError({ type: 'image/png', size: 100 }, 'data:image/png;base64,' + 'A'.repeat(MAX_ICON))).toMatch(
			/64 kB/
		);
		expect(iconError({ type: 'image/png', size: 100 }, 'data:image/png;base64,AAAA')).toBeNull();
	});
});

describe('appFormError', () => {
	const ok = { name: 'Jellyfin', url: 'https://jellyfin.home.lan', color: '#7b5cff' };
	it('accepts a valid tile', () => expect(appFormError(ok)).toBeNull());
	it('mirrors the server rules', () => {
		expect(appFormError({ ...ok, name: '  ' })?.field).toBe('name');
		expect(appFormError({ ...ok, name: 'x'.repeat(41) })?.field).toBe('name');
		expect(appFormError({ ...ok, url: 'jellyfin.home.lan' })?.field).toBe('url');
		expect(appFormError({ ...ok, url: 'javascript:alert(1)' })?.field).toBe('url');
		expect(appFormError({ ...ok, color: 'red' })?.field).toBe('color');
	});
});

describe('parseList', () => {
	it('splits on newlines, commas and spaces and drops blanks', () => {
		expect(parseList(' 10.0.0.0/8\n\n192.168.1.0/24, fd00::/8 ')).toEqual(['10.0.0.0/8', '192.168.1.0/24', 'fd00::/8']);
		expect(parseList('   ')).toEqual([]);
	});
});
