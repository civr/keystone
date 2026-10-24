/**
 * Labels, help, input kind and "does this lower security?" for every field of
 * keystone-core's Settings JSON (settings.Defaults() shape). Settings ›
 * Security renders entirely from this table; settingsMeta.test.ts proves it
 * covers every field in fixtures/defaults.json (`make web-fixtures`).
 */

export interface Settings {
	domain: string;
	public_url: string;
	session: {
		duration_hours: number;
		trusted_device_days: number;
		idle_timeout_minutes: number;
		sliding: boolean;
		same_site: string;
	};
	login: {
		passkey_required: boolean;
		passkey_only_on_trusted_device: boolean;
		user_verification: string;
		step_up_minutes: number;
	};
	brute_force: {
		login_per_ip_per_minute: number;
		login_per_account_per_minute: number;
		lockout_threshold: number;
		lockout_minutes: number;
	};
	network: { login_cidrs: string[]; admin_cidrs: string[]; trusted_proxies: string[] };
	headers: {
		hsts: boolean;
		hsts_include_subdomains: boolean;
		hsts_preload: boolean;
		frame_deny: boolean;
		hsts_max_age: number;
		csp_extra: string;
	};
	audit: { retention_days: number; syslog_enabled: boolean; syslog_addr: string; syslog_proto: string };
	tokens: { access_minutes: number; id_minutes: number; refresh_days: number };
	ldap: { require_tls: boolean; allowed_cidrs: string[]; bind_per_ip_per_minute: number };
	gate: { fail_per_ip_per_minute: number };
	vault: { reconfirm_minutes: number; clipboard_clear_seconds: number; allow_reveal: boolean };
}

export type Kind = 'bool' | 'int' | 'enum' | 'cidrs' | 'text';

export interface FieldMeta {
	label: string;
	help: string;
	kind: Kind;
	min?: number;
	max?: number;
	/** Shown after a number, e.g. "hours". */
	unit?: string;
	options?: { value: string; label: string }[];
	placeholder?: string;
	/**
	 * True when changing old → new lowers security; saving then asks for
	 * confirmation. (Method syntax: each rule takes its field's own value type.)
	 */
	weakens?(old: unknown, next: unknown): boolean;
}

// Reusable "weaker when …" rules.
const turnedOff = (o: boolean, n: boolean) => o && !n;
const turnedOn = (o: boolean, n: boolean) => !o && n;
const raised = (o: number, n: number) => n > o;
const lowered = (o: number, n: number) => n < o;
/** 0 means "off/unlimited": switching to 0 or raising a non-zero limit weakens. */
const limitLoosened = (o: number, n: number) => o > 0 && (n === 0 || n > o);
const added = (o: string[], n: string[]) => n.some((c) => !o.includes(c));
/** An allowlist that is emptied allows everyone; one that gains entries allows more. */
const allowlistWidened = (o: string[], n: string[]) => o.length > 0 && (n.length === 0 || added(o, n));

export const settingsMeta: Record<string, FieldMeta> = {
	domain: {
		label: 'Domain',
		help: 'Your home network’s domain, for example home.lan.',
		kind: 'text',
		placeholder: 'home.lan'
	},
	public_url: {
		label: 'Public address',
		help: 'The address Keystone is reached on. Apps and sign-in links use it.',
		kind: 'text',
		placeholder: 'https://auth.home.lan'
	},

	'session.duration_hours': {
		label: 'Session length',
		help: 'How long you stay signed in.',
		kind: 'int',
		min: 1,
		max: 8760,
		unit: 'hours',
		weakens: raised
	},
	'session.trusted_device_days': {
		label: 'Trusted device',
		help: 'How long a device counts as trusted after signing in. 0 = never.',
		kind: 'int',
		min: 0,
		max: 365,
		unit: 'days',
		weakens: raised
	},
	'session.idle_timeout_minutes': {
		label: 'Sign out when idle',
		help: 'Sign out after this long without activity. 0 = off.',
		kind: 'int',
		min: 0,
		max: 43200,
		unit: 'minutes',
		weakens: limitLoosened
	},
	'session.sliding': {
		label: 'Extend session while in use',
		help: 'The session length counts from your last activity instead of from signing in.',
		kind: 'bool',
		weakens: turnedOn
	},
	'session.same_site': {
		label: 'SameSite cookie',
		help: 'Strict never sends the session cookie from other sites; Lax does for plain links.',
		kind: 'enum',
		options: [
			{ value: 'Lax', label: 'Lax' },
			{ value: 'Strict', label: 'Strict' }
		],
		weakens: (o: string, n: string) => o === 'Strict' && n === 'Lax'
	},

	'login.passkey_required': {
		label: 'Require a passkey',
		help: 'A passkey is always needed after the password.',
		kind: 'bool',
		weakens: turnedOff
	},
	'login.passkey_only_on_trusted_device': {
		label: 'Passkey only on trusted devices',
		help: 'On a trusted device the passkey is enough; no password needed.',
		kind: 'bool',
		weakens: turnedOn
	},
	'login.user_verification': {
		label: 'User verification',
		help: 'Required: the passkey always asks for Face ID, a fingerprint or a PIN.',
		kind: 'enum',
		options: [
			{ value: 'required', label: 'Required' },
			{ value: 'preferred', label: 'When possible' }
		],
		weakens: (o: string, n: string) => o === 'required' && n !== 'required'
	},
	'login.step_up_minutes': {
		label: 'Confirm again after',
		help: 'Changes in Settings ask you to confirm again when your last confirmation is older than this.',
		kind: 'int',
		min: 1,
		max: 60,
		unit: 'minutes',
		weakens: raised
	},

	'brute_force.login_per_ip_per_minute': {
		label: 'Attempts per IP address',
		help: 'Most sign-in attempts per minute from one address.',
		kind: 'int',
		min: 1,
		max: 1000,
		unit: 'per minute',
		weakens: raised
	},
	'brute_force.login_per_account_per_minute': {
		label: 'Attempts per account',
		help: 'Most sign-in attempts per minute for one account.',
		kind: 'int',
		min: 1,
		max: 1000,
		unit: 'per minute',
		weakens: raised
	},
	'brute_force.lockout_threshold': {
		label: 'Lock after',
		help: 'Failed attempts after which signing in is locked for a while.',
		kind: 'int',
		min: 1,
		max: 1000,
		unit: 'attempts',
		weakens: raised
	},
	'brute_force.lockout_minutes': {
		label: 'Lock duration',
		help: 'How long signing in stays locked.',
		kind: 'int',
		min: 1,
		max: 1000,
		unit: 'minutes',
		weakens: lowered
	},

	'network.login_cidrs': {
		label: 'Sign-in allowed from',
		help: 'Networks (CIDR) that may sign in. Empty = anywhere.',
		kind: 'cidrs',
		placeholder: '192.168.1.0/24',
		weakens: allowlistWidened
	},
	'network.admin_cidrs': {
		label: 'Settings allowed from',
		help: 'Networks (CIDR) that may open Settings. Empty = anywhere.',
		kind: 'cidrs',
		placeholder: '192.168.1.0/24',
		weakens: allowlistWidened
	},
	'network.trusted_proxies': {
		label: 'Trusted proxies',
		help: 'Proxies whose X-Forwarded-For Keystone believes. With a reverse proxy in front of Keystone (for https), add the address it reaches Keystone from, often the Docker gateway such as 172.18.0.1/32. You see that address below as your address, and with your sessions under Devices.',
		kind: 'cidrs',
		placeholder: '10.0.0.2/32',
		weakens: added
	},

	'headers.hsts': {
		label: 'HSTS',
		help: 'Browsers use only https for Keystone from then on.',
		kind: 'bool',
		weakens: turnedOff
	},
	'headers.hsts_include_subdomains': {
		label: 'HSTS for subdomains',
		help: 'Also applies to every subdomain of this address.',
		kind: 'bool',
		weakens: turnedOff
	},
	'headers.hsts_preload': {
		label: 'HSTS preload',
		help: 'Allows inclusion in the browsers’ preload list. Public domains only.',
		kind: 'bool',
		weakens: turnedOff
	},
	'headers.frame_deny': {
		label: 'Block framing',
		help: 'Other sites may not show Keystone in a frame (protects against clickjacking).',
		kind: 'bool',
		weakens: turnedOff
	},
	'headers.hsts_max_age': {
		label: 'HSTS duration',
		help: 'How long browsers remember to use https. 31536000 = one year.',
		kind: 'int',
		min: 0,
		max: 63072000,
		unit: 'seconds',
		weakens: lowered
	},
	'headers.csp_extra': {
		label: 'Extra CSP directives',
		help: 'Appended to the Content-Security-Policy. Leave empty unless you know why.',
		kind: 'text',
		placeholder: "img-src 'self' https://cdn.example",
		weakens: (o: string, n: string) => n.trim() !== '' && n.trim() !== o.trim()
	},

	'audit.retention_days': {
		label: 'Keep activity',
		help: 'How long the activity log is kept. 0 = forever.',
		kind: 'int',
		min: 0,
		max: 3650,
		unit: 'days',
		weakens: (o: number, n: number) => n > 0 && (o === 0 || n < o)
	},
	'audit.syslog_enabled': {
		label: 'Forward to syslog',
		help: 'Also send every event to a syslog server.',
		kind: 'bool',
		weakens: turnedOff
	},
	'audit.syslog_addr': {
		label: 'Syslog server',
		help: 'Host and port, for example 192.168.1.10:514.',
		kind: 'text',
		placeholder: '192.168.1.10:514'
	},
	'audit.syslog_proto': {
		label: 'Syslog protocol',
		help: 'TLS encrypts the traffic to the syslog server.',
		kind: 'enum',
		options: [
			{ value: 'udp', label: 'UDP' },
			{ value: 'tcp', label: 'TCP' },
			{ value: 'tls', label: 'TLS' }
		],
		weakens: (o: string, n: string) => o === 'tls' && n !== 'tls'
	},
	'tokens.access_minutes': {
		label: 'Access token lifetime',
		help: 'How long an app’s OIDC access token stays valid.',
		kind: 'int',
		min: 1,
		max: 1440,
		unit: 'minutes',
		weakens: raised
	},
	'tokens.id_minutes': {
		label: 'ID token lifetime',
		help: 'How long an OIDC ID token is valid.',
		kind: 'int',
		min: 1,
		max: 1440,
		unit: 'minutes',
		weakens: raised
	},
	'tokens.refresh_days': {
		label: 'Refresh token lifetime',
		help: 'How long an app may quietly extend your session. Always ends with your Keystone session. 0 = no refresh tokens.',
		kind: 'int',
		min: 0,
		max: 365,
		unit: 'days',
		weakens: raised
	},

	'ldap.require_tls': {
		label: 'LDAPS only for apps',
		help: 'Apps must use keystone-ldap over LDAPS (port 636). Off also opens unencrypted LDAP on port 389.',
		kind: 'bool',
		weakens: turnedOff
	},
	'ldap.allowed_cidrs': {
		label: 'Allowed LDAP clients',
		help: 'Only these addresses may talk to keystone-ldap. Empty = everyone.',
		kind: 'cidrs',
		placeholder: '192.168.1.20/32',
		weakens: allowlistWidened
	},
	'ldap.bind_per_ip_per_minute': {
		label: 'LDAP binds per IP',
		help: 'Most LDAP sign-in attempts per address per minute.',
		kind: 'int',
		min: 1,
		max: 1000,
		unit: 'per minute',
		weakens: raised
	},

	'gate.fail_per_ip_per_minute': {
		label: 'Refused gate checks per IP',
		help: 'After this many refused requests per minute the gate blocks an address for a while.',
		kind: 'int',
		min: 1,
		max: 10000,
		unit: 'per minute',
		weakens: raised
	},

	'vault.reconfirm_minutes': {
		label: 'Confirm again after',
		help: 'Copying a password from the vault asks you to confirm first when your last confirmation is older than this.',
		kind: 'int',
		min: 1,
		max: 1440,
		unit: 'minutes',
		weakens: raised
	},
	'vault.clipboard_clear_seconds': {
		label: 'Clear clipboard after',
		help: 'A copied password is removed from the clipboard after this.',
		kind: 'int',
		min: 5,
		max: 600,
		unit: 'seconds',
		weakens: raised
	},
	'vault.allow_reveal': {
		label: 'Show passwords',
		help: 'Let a vault password be shown, not only copied.',
		kind: 'bool',
		weakens: turnedOn
	}
};

/** The Security page, top to bottom. Recovery (codes, emergency account) follows separately. */
export interface SettingsGroup {
	id: string;
	title: string;
	paths: string[];
	foot?: string;
	/** Show "Your address as Keystone sees it" under the group (from GET /me). */
	showClientIP?: boolean;
}

export const securityGroups: SettingsGroup[] = [
	{
		id: 'sessions',
		title: 'Sessions',
		paths: [
			'session.duration_hours',
			'session.sliding',
			'session.idle_timeout_minutes',
			'session.trusted_device_days',
			'session.same_site'
		]
	},
	{
		id: 'sign-in',
		title: 'Sign-in',
		paths: [
			'login.passkey_required',
			'login.passkey_only_on_trusted_device',
			'login.user_verification',
			'login.step_up_minutes'
		]
	},
	{
		id: 'brute-force',
		title: 'Brute-force',
		paths: [
			'brute_force.login_per_ip_per_minute',
			'brute_force.login_per_account_per_minute',
			'brute_force.lockout_threshold',
			'brute_force.lockout_minutes'
		]
	},
	{
		id: 'network',
		title: 'Network',
		paths: ['network.login_cidrs', 'network.admin_cidrs', 'network.trusted_proxies'],
		showClientIP: true
	},
	{
		id: 'headers',
		title: 'HTTP headers',
		paths: [
			'headers.hsts',
			'headers.hsts_max_age',
			'headers.hsts_include_subdomains',
			'headers.hsts_preload',
			'headers.frame_deny',
			'headers.csp_extra'
		]
	},
	{
		id: 'audit',
		title: 'Audit',
		paths: ['audit.retention_days', 'audit.syslog_enabled', 'audit.syslog_addr', 'audit.syslog_proto']
	},
	{
		id: 'tokens',
		title: 'Tokens (OIDC)',
		paths: ['tokens.access_minutes', 'tokens.id_minutes', 'tokens.refresh_days']
	},
	{
		id: 'ldap',
		title: 'LDAP for apps',
		paths: ['ldap.require_tls', 'ldap.allowed_cidrs', 'ldap.bind_per_ip_per_minute']
	},
	{
		id: 'gate',
		title: 'Gate',
		paths: ['gate.fail_per_ip_per_minute']
	},
	{
		id: 'vault',
		title: 'Vault',
		paths: ['vault.reconfirm_minutes', 'vault.clipboard_clear_seconds', 'vault.allow_reveal']
	}
];

type Tree = Record<string, unknown>;
const isObject = (v: unknown): v is Tree => typeof v === 'object' && v !== null && !Array.isArray(v);

/** Dotted paths of every leaf (lists count as leaves), in document order. */
export function leafPaths(obj: unknown, prefix = ''): string[] {
	if (!isObject(obj)) return [prefix];
	return Object.entries(obj).flatMap(([k, v]) => leafPaths(v, prefix ? `${prefix}.${k}` : k));
}

export function getPath(obj: unknown, path: string): unknown {
	return path.split('.').reduce<unknown>((o, k) => (isObject(o) ? o[k] : undefined), obj);
}

/** A copy of obj with path set to value; untouched branches are shared. */
export function setPath<T>(obj: T, path: string, value: unknown): T {
	const [head, ...rest] = path.split('.');
	const tree = obj as Tree;
	return { ...tree, [head]: rest.length ? setPath(tree[head], rest.join('.'), value) : value } as T;
}

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

/** Leaf paths whose value differs between a and b, in document order. */
export function changedPaths(a: unknown, b: unknown): string[] {
	return leafPaths(a).filter((p) => !same(getPath(a, p), getPath(b, p)));
}

/** Changed paths whose meta says the change lowers security, in settingsMeta order. */
export function weakenedPaths(a: unknown, b: unknown): string[] {
	return Object.entries(settingsMeta)
		.filter(([p, m]) => {
			const o = getPath(a, p);
			const n = getPath(b, p);
			return !same(o, n) && m.weakens?.(o, n) === true;
		})
		.map(([p]) => p);
}

/** Client-side check before saving (the server validates again): a message or null. */
export function fieldError(meta: FieldMeta, v: unknown): string | null {
	if (meta.kind !== 'int') return null;
	if (typeof v !== 'number' || !Number.isInteger(v)) return 'Enter a whole number.';
	if (v < meta.min! || v > meta.max!) return `Choose a value from ${meta.min} to ${meta.max}.`;
	return null;
}
