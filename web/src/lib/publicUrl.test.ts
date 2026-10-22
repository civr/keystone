import { describe, expect, it } from 'vitest';
import { passkeysNeedHttps } from './publicUrl';

describe('passkeysNeedHttps', () => {
	it('warns for http on a real host', () => {
		expect(passkeysNeedHttps('http://keystone.home.lan')).toBe(true);
		expect(passkeysNeedHttps(' HTTP://192.168.1.5:8080 ')).toBe(true);
	});
	it('is quiet for https, localhost, and input that is not a URL yet', () => {
		expect(passkeysNeedHttps('https://keystone.home.lan')).toBe(false);
		expect(passkeysNeedHttps('http://localhost:8080')).toBe(false);
		expect(passkeysNeedHttps('http://app.localhost')).toBe(false);
		expect(passkeysNeedHttps('http://127.0.0.1')).toBe(false);
		expect(passkeysNeedHttps('http://[::1]:8080')).toBe(false);
		expect(passkeysNeedHttps('')).toBe(false);
		expect(passkeysNeedHttps('keystone.home')).toBe(false);
	});
});
