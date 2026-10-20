import { describe, expect, it } from 'vitest';
import { greeting, firstName } from './greeting';

const at = (h: number, m: number) => new Date(2026, 9, 20, h, m, 0);

describe('greeting', () => {
	it.each([
		[0, 0, 'Goedenacht'],
		[5, 59, 'Goedenacht'],
		[6, 0, 'Goedemorgen'],
		[11, 59, 'Goedemorgen'],
		[12, 0, 'Goedemiddag'],
		[17, 59, 'Goedemiddag'],
		[18, 0, 'Goedenavond'],
		[23, 59, 'Goedenavond']
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
