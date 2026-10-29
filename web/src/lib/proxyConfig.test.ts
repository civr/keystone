import { describe, expect, it } from 'vitest';
import { proxyConfig } from './proxyConfig';

const o = { host: 'ha.home.lan', gate: '10.0.0.2:8081', keystone: 'https://ks.home.lan', signIn: '' };

describe('proxyConfig', () => {
	it('npm + jellyfin proxies /.keystone/ and has no Authorization', () => {
		const c = proxyConfig('npm', { ...o, signIn: 'jellyfin' });
		expect(c).toContain('location /.keystone/');
		expect(c).toContain('include conf.d/include/proxy.conf');
		expect(c).not.toContain('Authorization');
	});
	it('caddy + basic copies Authorization, no /.keystone/', () => {
		const c = proxyConfig('caddy', { ...o, signIn: 'basic' });
		expect(c).toMatch(/copy_headers .*Authorization/);
		expect(c).not.toContain('/.keystone/');
	});
	it('npm + header lowercases the header name', () => {
		expect(proxyConfig('npm', { ...o, signIn: 'header', header: 'X-Api-Key' })).toContain('$upstream_http_x_api_key');
	});
});
