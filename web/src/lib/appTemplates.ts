import type { ProtocolForm } from './protocols';

/**
 * Ready-made app settings: pick one, enter the app's address, and the
 * protocol, redirect URIs and tile address follow. `hint` says what to do
 * in the app itself.
 */
export interface AppTemplate {
	id: string;
	name: string;
	color: string;
	hint: string;
	/** The tile's address from the app's base address. */
	tile?: (base: string) => string;
	apply: (base: string, f: ProtocolForm) => void;
}

const host = (base: string) => {
	try {
		return new URL(base).hostname;
	} catch {
		return '';
	}
};

const oidc = (redirect: (b: string) => string) => (base: string, f: ProtocolForm) => {
	f.protocol = 'oidc';
	f.oidc.redirect_uris = redirect(base);
};

export const TEMPLATES: AppTemplate[] = [
	{
		id: 'jellyfin',
		name: 'Jellyfin',
		color: '#00a4dc',
		hint: 'In Jellyfin install the SSO Authentication plugin and add an OID provider named “keystone” with the client ID and secret shown after saving.',
		tile: (b) => `${b}/sso/OID/start/keystone`,
		apply: oidc((b) => `${b}/sso/OID/redirect/keystone`)
	},
	{
		id: 'homeassistant',
		name: 'Home Assistant',
		color: '#18bcf2',
		hint: 'Put Home Assistant behind your reverse proxy with forward auth to keystone-gate (README › Gate); its apps keep working through the exceptions.',
		apply: (b, f) => {
			f.protocol = 'gate';
			f.gate.host = host(b);
			f.gate.bypass = '/api/\n/auth/token';
		}
	},
	{
		id: 'grafana',
		name: 'Grafana',
		color: '#f46800',
		hint: 'In grafana.ini enable [auth.generic_oauth] with the client ID and secret, and the issuer’s authorize, token and userinfo addresses.',
		apply: oidc((b) => `${b}/login/generic_oauth`)
	},
	{
		id: 'proxmox',
		name: 'Proxmox VE',
		color: '#e57000',
		hint: 'Datacenter › Permissions › Realms › Add › OpenID Connect: issuer is Keystone’s address, with the client ID and secret.',
		apply: oidc((b) => b)
	},
	{
		id: 'nextcloud',
		name: 'Nextcloud',
		color: '#0082c9',
		hint: 'Install “OpenID Connect user backend” (user_oidc) and register a provider with Keystone’s discovery address.',
		apply: oidc((b) => `${b}/apps/user_oidc/code`)
	},
	{
		id: 'gitea',
		name: 'Gitea / Forgejo',
		color: '#609926',
		hint: 'Site administration › Authentication sources › Add › OAuth2 › OpenID Connect, named “keystone”.',
		apply: oidc((b) => `${b}/user/oauth2/keystone/callback`)
	},
	{
		id: 'portainer',
		name: 'Portainer',
		color: '#13bef9',
		hint: 'Settings › Authentication › OAuth › Custom with Keystone’s authorize, token and userinfo addresses.',
		apply: oidc((b) => b)
	},
	{
		id: 'nimbus',
		name: 'Project Nimbus',
		color: '#5856d6',
		hint: 'In Nimbus: Settings › Sign-in › generic OIDC with Keystone’s address, the client ID and secret.',
		apply: oidc((b) => `${b}/api/auth/sso/callback`)
	},
	{
		id: 'bastion',
		name: 'Bastion',
		color: '#1d1d1f',
		hint: 'In Bastion: Admin › Keystone (SAML), with Keystone’s IdP metadata address.',
		apply: (b, f) => {
			f.protocol = 'saml';
			f.saml.entity_id = `${b}/api/auth/saml/metadata`;
			f.saml.acs_url = `${b}/api/auth/saml/acs`;
		}
	},
	{
		id: 'router',
		name: 'Router or NAS',
		color: '#8e8e93',
		hint: 'Keystone keeps the username and password; set “Sign in for me” with the login form’s address if the page is a classic form.',
		apply: (_b, f) => {
			f.protocol = 'vault';
		}
	}
];
