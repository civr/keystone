import { describe, expect, it } from 'vitest';
import { connectionInfo, protocolError, protocolForm, protocolPayload, slugify } from './protocols';
import { tileHref, type App } from './apps';

const base: App = {
	id: 'a1',
	name: 'Jellyfin',
	url: 'https://jf.home.lan',
	color: '#00a4dc',
	icon: null,
	protocol: 'link',
	config: {},
	sort: 0,
	created_at: ''
};

describe('protocol form', () => {
	it('round-trips an app config', () => {
		const app: App = {
			...base,
			protocol: 'gate',
			config: { account: { username: 'thijs_ha', email: '', display_name: '' }, gate: { host: 'ha.home.lan', bypass_paths: ['/api/', '/auth/token'] } }
		};
		const f = protocolForm(app);
		expect(f.gate.bypass).toBe('/api/\n/auth/token');
		expect(protocolPayload(f)).toEqual({
			protocol: 'gate',
			config: { account: { username: 'thijs_ha', email: '', display_name: '' }, gate: { host: 'ha.home.lan', bypass_paths: ['/api/', '/auth/token'] } }
		});
	});

	it('sends only the chosen protocol', () => {
		const f = protocolForm(null);
		f.protocol = 'oidc';
		f.oidc.redirect_uris = 'https://n.lan/cb\n\nhttps://n.lan/cb2 ';
		f.saml.entity_id = 'left over';
		const p = protocolPayload(f);
		expect(p.config.saml).toBeUndefined();
		expect(p.config.oidc).toEqual({ redirect_uris: ['https://n.lan/cb', 'https://n.lan/cb2'] });
	});

	it('validates like the server', () => {
		const f = protocolForm(null);
		f.protocol = 'saml';
		expect(protocolError(f)?.field).toBe('config.saml.entity_id');
		f.saml.entity_id = 'bastion';
		f.saml.acs_url = 'http://bastion.example.com/acs';
		expect(protocolError(f)?.field).toBe('config.saml.acs_url');
		f.saml.acs_url = 'http://bastion.home.lan/acs';
		expect(protocolError(f)).toBeNull();
		f.protocol = 'ldap';
		f.ldap.slug = 'Jelly fin';
		expect(protocolError(f)?.field).toBe('config.ldap.slug');
		f.protocol = 'gate';
		f.gate.host = 'https://ha.home.lan';
		expect(protocolError(f)?.field).toBe('config.gate.host');
	});

	it('slugifies names for LDAP', () => {
		expect(slugify('Jellyfin Média!')).toBe('jellyfin-media');
		expect(slugify('')).toBe('app');
	});
});

describe('connection info', () => {
	it('lists what the app needs per protocol', () => {
		const origin = 'https://keystone.home.lan';
		const saml = connectionInfo(origin, { ...base, protocol: 'saml', config: { saml: { entity_id: 'x', acs_url: 'https://b.lan/acs', idp_initiated: false } } });
		expect(saml.map((r) => r.value)).toContain('https://keystone.home.lan/saml/metadata');
		const oidc = connectionInfo(origin, { ...base, protocol: 'oidc', config: { oidc: { client_id: 'cid', redirect_uris: [] } } });
		expect(oidc).toContainEqual({ label: 'Issuer', value: origin });
		expect(oidc).toContainEqual({ label: 'Client ID', value: 'cid' });
		const ldap = connectionInfo(origin, { ...base, protocol: 'ldap', config: { ldap: { slug: 'jellyfin' }, account: { username: 't.willems', email: '', display_name: '' } } });
		expect(ldap).toContainEqual({ label: 'Bind DN', value: 'cn=bind,ou=jellyfin,dc=keystone' });
		expect(ldap).toContainEqual({ label: 'Server', value: 'ldaps://keystone.home.lan:636' });
		expect(ldap).toContainEqual({ label: 'Search base', value: 'ou=jellyfin,dc=keystone' });
		const gate = connectionInfo(origin, { ...base, protocol: 'gate', config: { gate: { host: 'ha.home.lan', bypass_paths: [] } } });
		expect(gate).toContainEqual({ label: 'Forward-auth address', value: 'http://keystone.home.lan:8081/verify' });
	});
});

describe('tileHref', () => {
	it('uses the IdP-initiated endpoint only when enabled', () => {
		const saml: App = { ...base, protocol: 'saml', config: { saml: { entity_id: 'x', acs_url: 'https://b/acs', idp_initiated: true } } };
		expect(tileHref(saml)).toBe('/saml/idp/a1');
		expect(tileHref({ ...saml, config: { saml: { ...saml.config.saml!, idp_initiated: false } } })).toBe('https://jf.home.lan');
		expect(tileHref({ ...base, url: 'javascript:alert(1)' })).toBeNull();
	});
});
