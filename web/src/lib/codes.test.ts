import { describe, expect, it } from 'vitest';
import { codesFile, formatCode } from './codes';

describe('formatCode', () => {
	it('uppercases and groups by four as you type', () => {
		expect(formatCode('ab')).toBe('AB');
		expect(formatCode('abcd')).toBe('ABCD');
		expect(formatCode('abcde')).toBe('ABCD-E');
		expect(formatCode('abcd-efgh ijkl.mnop')).toBe('ABCD-EFGH-IJKL-MNOP');
	});

	it('stops at sixteen characters', () => {
		expect(formatCode('ABCDEFGHJKMNPQRSTVWX')).toBe('ABCD-EFGH-JKMN-PQRS');
	});
});

describe('codesFile', () => {
	it('lists every code on its own line with a header', () => {
		const txt = codesFile(['AAAA-BBBB-CCCC-DDDD', 'EEEE-FFFF-GGGG-HHHH'], 'https://keystone.home.lan', new Date(2026, 9, 20));
		expect(txt).toContain('Keystone recovery codes');
		expect(txt).toContain('https://keystone.home.lan');
		expect(txt).toContain('20 October 2026');
		expect(txt).toMatch(/^ {2}1\. AAAA-BBBB-CCCC-DDDD$/m);
		expect(txt).toMatch(/^ {2}2\. EEEE-FFFF-GGGG-HHHH$/m);
		expect(txt.endsWith('\n')).toBe(true);
	});
});
