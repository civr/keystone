import { describe, expect, it } from 'vitest';
import { DEFAULT_BRAND, brandError, luminance } from './brand.svelte';

describe('brandError', () => {
	it('accepts the defaults', () => {
		expect(brandError(DEFAULT_BRAND)).toEqual({});
	});
	it.each([
		['name', { name: '  ' }],
		['name', { name: 'x'.repeat(41) }],
		['accent', { accent: 'blue' }],
		['accent', { accent: '#ffd60a' }],
		['background', { background: '#222222' }]
	])('flags %s', (field, edit) => {
		expect(Object.keys(brandError({ ...DEFAULT_BRAND, ...edit }))).toEqual([field]);
	});
});

describe('luminance', () => {
	it('spans black to white', () => {
		expect(luminance('#000000')).toBe(0);
		expect(luminance('#ffffff')).toBeCloseTo(1);
	});
});
