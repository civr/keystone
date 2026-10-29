export interface App {
	id: string;
	name: string;
	url: string;
	color: string;
	/** data: URL or null; only ever rendered through <img src>. */
	icon: string | null;
	protocol: Protocol;
	config: AppConfig;
	/** OIDC client secret or LDAP bind password has been generated. */
	has_secret?: boolean;
	/** Gate apps that sign in for you: the address that signs in first, then opens the app. */
	launch_url?: string;
	/** Account ids that may use it; null is everyone. */
	access?: string[] | null;
	sort: number;
	created_at: string;
}

export type Protocol = 'link' | 'saml' | 'oidc' | 'ldap' | 'gate' | 'vault';

export interface AppConfig {
	account?: { username: string; email: string; display_name: string };
	saml?: { entity_id: string; acs_url: string; idp_initiated: boolean };
	oidc?: { client_id: string; redirect_uris: string[] };
	ldap?: { slug: string };
	gate?: { host: string; bypass_paths: string[]; sign_in?: GateSignIn };
	/** How a vault tile signs in: copy the password, or post the login form. */
	vault?: { mode: VaultMode; login_url?: string; user_field?: string; pass_field?: string };
	/** Only sessions signed in with a passkey or authenticator code. */
	require_mfa?: boolean;
}

/** How keystone-gate signs in to a gate app with the vault's details. */
export interface GateSignIn {
	mode: GateSignInMode;
	upstream?: string;
	login_path?: string;
	user_field?: string;
	pass_field?: string;
	header?: string;
}

export type GateSignInMode = 'basic' | 'header' | 'form' | 'json' | 'jellyfin';

export type VaultMode = 'copy' | 'form' | 'json';

/**
 * Where a tile goes: Keystone's IdP-initiated SAML endpoint when the app
 * accepts unsolicited responses, otherwise the app's own (sign-in) address.
 */
export function tileHref(app: App): string | null {
	if (app.launch_url) return safeHref(app.launch_url);
	if (app.protocol === 'saml' && app.config?.saml?.idp_initiated) return `/saml/idp/${encodeURIComponent(app.id)}`;
	return safeHref(app.url);
}

/** Lowercase and strip diacritics so "Pröxmox" and "proxmox" compare equal. */
export function fold(s: string): string {
	return s.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase();
}

export function hostOf(url: string): string {
	try {
		return new URL(url).hostname;
	} catch {
		return '';
	}
}

/** Apps whose name or URL host contains query (case- and diacritic-insensitive). */
export function filterApps(apps: App[], query: string): App[] {
	const q = fold(query.trim());
	if (!q) return apps;
	return apps.filter((a) => fold(a.name).includes(q) || fold(hostOf(a.url)).includes(q));
}

/** url when it is an absolute http(s) URL, else null — never link javascript: or data:. */
export function safeHref(url: string): string | null {
	try {
		const { protocol } = new URL(url);
		return protocol === 'http:' || protocol === 'https:' ? url : null;
	} catch {
		return null;
	}
}
