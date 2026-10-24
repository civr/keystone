/** Per-protocol app settings for Instellingen › Apps (mirrors internal/apps). */
import type { App, AppConfig, Protocol } from './apps';

export const PROTOCOLS: { value: Protocol; label: string; help: string }[] = [
	{ value: 'link', label: 'Link', help: 'Just a tile; the app handles sign-in itself.' },
	{ value: 'saml', label: 'SAML', help: 'Keystone signs you in with SAML 2.0 (for example Bastion).' },
	{ value: 'oidc', label: 'OpenID Connect', help: 'Keystone is the app’s OIDC provider (for example Project Nimbus).' },
	{ value: 'ldap', label: 'LDAP', help: 'The app checks your password through keystone-ldap (for example Jellyfin).' },
	{ value: 'gate', label: 'Gate', help: 'keystone-gate lets only signed-in people through and tells the app who they are (for example Home Assistant).' },
	{ value: 'vault', label: 'Vault', help: 'Keystone keeps the username and password and copies the password when you open the app.' }
];

/** Suggested gate exceptions for native apps (spec §10). */
export const BYPASS_PRESETS: { name: string; paths: string[] }[] = [
	{ name: 'Home Assistant', paths: ['/api/', '/auth/token'] },
	{ name: 'Jellyfin', paths: ['/Users/AuthenticateByName', '/socket', '/emby/'] }
];

/** Editable form state: lists as one-per-line text. */
export interface ProtocolForm {
	protocol: Protocol;
	account: { username: string; email: string; display_name: string };
	saml: { entity_id: string; acs_url: string; idp_initiated: boolean };
	oidc: { redirect_uris: string };
	ldap: { slug: string };
	gate: { host: string; bypass: string };
}

export function protocolForm(app: App | null): ProtocolForm {
	const c: AppConfig = app?.config ?? {};
	return {
		protocol: app?.protocol ?? 'link',
		account: { username: '', email: '', display_name: '', ...c.account },
		saml: { entity_id: '', acs_url: '', idp_initiated: false, ...c.saml },
		oidc: { redirect_uris: (c.oidc?.redirect_uris ?? []).join('\n') },
		ldap: { slug: c.ldap?.slug ?? '' },
		gate: { host: c.gate?.host ?? '', bypass: (c.gate?.bypass_paths ?? []).join('\n') }
	};
}

const lines = (s: string) => s.split('\n').map((l) => l.trim()).filter(Boolean);

/** The protocol and config to send; only the chosen protocol's section. */
export function protocolPayload(f: ProtocolForm): { protocol: Protocol; config: AppConfig } {
	const a = f.account;
	const config: AppConfig = { account: { username: a.username.trim(), email: a.email.trim(), display_name: a.display_name.trim() } };
	switch (f.protocol) {
		case 'saml':
			config.saml = { entity_id: f.saml.entity_id.trim(), acs_url: f.saml.acs_url.trim(), idp_initiated: f.saml.idp_initiated };
			break;
		case 'oidc':
			config.oidc = { redirect_uris: lines(f.oidc.redirect_uris) } as AppConfig['oidc'];
			break;
		case 'ldap':
			config.ldap = { slug: f.ldap.slug.trim() };
			break;
		case 'gate':
			config.gate = { host: f.gate.host.trim().toLowerCase(), bypass_paths: lines(f.gate.bypass) };
			break;
	}
	return { protocol: f.protocol, config };
}

const LOCAL = ['.lan', '.local', '.home.arpa', '.internal', '.localhost'];

/** Absolute URL without fragment; plain http only for home-network hosts. */
function appUrl(raw: string): boolean {
	let u: URL;
	try {
		u = new URL(raw);
	} catch {
		return false;
	}
	if (u.hash || u.username) return false;
	if (u.protocol === 'https:') return true;
	if (u.protocol !== 'http:') return false;
	const h = u.hostname.toLowerCase();
	return /^[\d.]+$/.test(h) || h.startsWith('[') || LOCAL.some((s) => h.endsWith(s) || h === s.slice(1));
}

/** First problem as {field, message} using the API's field names, or null. */
export function protocolError(f: ProtocolForm): { field: string; message: string } | null {
	const p = protocolPayload(f);
	const c = p.config;
	if (p.protocol === 'saml') {
		if (!c.saml!.entity_id) return { field: 'config.saml.entity_id', message: 'Enter the app’s entity ID.' };
		if (!appUrl(c.saml!.acs_url))
			return { field: 'config.saml.acs_url', message: 'The ACS URL must be absolute; http only for addresses on your home network.' };
	}
	if (p.protocol === 'oidc') {
		const uris = c.oidc!.redirect_uris;
		if (uris.length < 1 || uris.length > 10) return { field: 'config.oidc.redirect_uris', message: "Vul 1 tot 10 redirect-URI's in." };
		if (!uris.every(appUrl))
			return { field: 'config.oidc.redirect_uris', message: 'Each redirect URI must be an absolute URL without a # part; http only on your home network.' };
	}
	if (p.protocol === 'ldap' && !/^[a-z0-9][a-z0-9-]{0,31}$/.test(c.ldap!.slug))
		return { field: 'config.ldap.slug', message: 'Use 1 to 32 lowercase letters, digits or dashes.' };
	if (p.protocol === 'gate') {
		if (!/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/.test(c.gate!.host))
			return { field: 'config.gate.host', message: 'Enter the app’s host name, such as ha.home.lan.' };
		if (!c.gate!.bypass_paths.every((x) => x.startsWith('/') && !/[\s?#]/.test(x)))
			return { field: 'config.gate.bypass_paths', message: 'Each exception is a path starting with /.' };
	}
	return null;
}

/** "Jellyfin Média!" → "jellyfin-media" (an LDAP subtree name). */
export function slugify(name: string): string {
	const s = name
		.normalize('NFD')
		.replace(/\p{M}/gu, '')
		.toLowerCase()
		.replace(/[^a-z0-9]+/g, '-')
		.replace(/^-+|-+$/g, '')
		.slice(0, 32);
	return s || 'app';
}

/** Values to copy into the app's own settings, per protocol. */
export function connectionInfo(origin: string, app: App): { label: string; value: string }[] {
	const host = new URL(origin).hostname;
	const c = app.config ?? {};
	switch (app.protocol) {
		case 'saml':
			return [
				{ label: 'IdP metadata', value: `${origin}/saml/metadata` },
				{ label: 'IdP entity ID', value: `${origin}/saml/metadata` },
				{ label: 'SSO address', value: `${origin}/saml/sso` }
			];
		case 'oidc':
			return [
				{ label: 'Issuer', value: origin },
				{ label: 'Discovery', value: `${origin}/.well-known/openid-configuration` },
				{ label: 'Client ID', value: c.oidc?.client_id ?? '' }
			];
		case 'ldap': {
			const ou = `ou=${c.ldap?.slug ?? ''},dc=keystone`;
			return [
				{ label: 'Server', value: `ldaps://${host}:636` },
				{ label: 'Bind DN', value: `cn=bind,${ou}` },
				{ label: 'Search base', value: ou },
				{ label: 'User filter', value: '(uid={0})' }
			];
		}
		case 'gate':
			return [
				{ label: 'Forward-auth address', value: `http://${host}:8081/verify` },
				{ label: 'Headers to the app', value: 'X-Keystone-User, X-Keystone-Email, X-Keystone-Name' }
			];
	}
	return [];
}
