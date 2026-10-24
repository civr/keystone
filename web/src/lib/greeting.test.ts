import { describe, expect, it } from 'vitest';
import { greeting, firstName } from './greeting';

const at = (h: number, m: number) => new Date(2026, 9, 20, h, m, 0);

describe('greeting', () => {
	it.each([
		[0, 0, 'Good night'],
		[5, 59, 'Good night'],
		[6, 0, 'Good morning'],
		[11, 59, 'Good morning'],
		[12, 0, 'Good afternoon'],
		[17, 59, 'Good afternoon'],
		[18, 0, 'Good evening'],
		[23, 59, 'Good evening']
	])('%i:%i → %s', (h, m, want) => {
		expect(greeting(at(h, m))).toBe(want);
	});
});

describe('firstName', () => {
	it('prefers given_name, then display_name, then username', () => {
		expect(firstName({ username: 'thijs', display_name: 'Thijs W', given_name: 'Thijs' })).toBe('Thijs');
		expect(firstName({ username: 'thijs', display_name: 'Thijs W', given_name: '' })).toBe('Thijs W');
		expect(firstName({ username: 'thijs', display_name: '  ', given_name: null })).toBe('thijs');
	});
});
