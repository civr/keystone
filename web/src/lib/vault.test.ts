import { describe, expect, it } from 'vitest';
import { filterItems, strongPassword, type VaultItem } from './vault';

const item = (title: string, username = '', url = ''): VaultItem => ({
	id: title,
	title,
	url,
	username,
	has_password: true,
	has_note: false,
	kind: 'login',
	updated_at: ''
});

describe('filterItems', () => {
	const items = [item('Router', 'admin', 'http://192.168.1.1'), item('NAS', 'thijs'), item('Wi-Fi')];
	it('matches title, username and address', () => {
		expect(filterItems(items, 'rout').map((i) => i.title)).toEqual(['Router']);
		expect(filterItems(items, 'THIJS').map((i) => i.title)).toEqual(['NAS']);
		expect(filterItems(items, '192.168').map((i) => i.title)).toEqual(['Router']);
		expect(filterItems(items, '  ')).toHaveLength(3);
	});
});

describe('strongPassword', () => {
	it('is three hyphenated groups of six with a capital and a digit', () => {
		for (let i = 0; i < 50; i++) {
			const p = strongPassword();
			expect(p).toMatch(/^[a-zA-Z0-9]{6}-[a-zA-Z0-9]{6}-[a-zA-Z0-9]{6}$/);
			expect(p.replace(/-/g, '')).toMatch(/[A-Z]/);
			expect(p).toMatch(/[0-9]/);
		}
	});
	it('differs between calls', () => {
		expect(strongPassword()).not.toBe(strongPassword());
	});
});
