/**
 * Labels, help, input kind and "does this lower security?" for every field of
 * keystone-core's Settings JSON (settings.Defaults() shape). Instellingen ›
 * Beveiliging renders entirely from this table; settingsMeta.test.ts proves it
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
}

export type Kind = 'bool' | 'int' | 'enum' | 'cidrs' | 'text';

export interface FieldMeta {
	label: string;
	help: string;
	kind: Kind;
	min?: number;
	max?: number;
	/** Shown after a number, e.g. "uur". */
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
		label: 'Domein',
		help: 'Het domein van je thuisnetwerk, bijvoorbeeld home.lan.',
		kind: 'text',
		placeholder: 'home.lan'
	},
	public_url: {
		label: 'Publiek adres',
		help: 'Het adres waarop Keystone bereikbaar is. Passkeys zijn aan deze hostnaam gekoppeld.',
		kind: 'text',
		placeholder: 'https://keystone.home.lan'
	},

	'session.duration_hours': {
		label: 'Sessieduur',
		help: 'Hoe lang je aangemeld blijft.',
		kind: 'int',
		min: 1,
		max: 8760,
		unit: 'uur',
		weakens: raised
	},
	'session.trusted_device_days': {
		label: 'Vertrouwd apparaat',
		help: 'Hoe lang een apparaat na inloggen als vertrouwd geldt. 0 = nooit.',
		kind: 'int',
		min: 0,
		max: 365,
		unit: 'dagen',
		weakens: raised
	},
	'session.idle_timeout_minutes': {
		label: 'Afmelden na inactiviteit',
		help: 'Meld af als je zo lang niets doet. 0 = uit.',
		kind: 'int',
		min: 0,
		max: 43200,
		unit: 'minuten',
		weakens: limitLoosened
	},
	'session.sliding': {
		label: 'Sessie verlengen bij gebruik',
		help: 'De sessieduur telt vanaf je laatste activiteit in plaats van vanaf het inloggen.',
		kind: 'bool',
		weakens: turnedOn
	},
	'session.same_site': {
		label: 'SameSite-cookie',
		help: 'Strict stuurt de sessiecookie nooit mee vanaf andere sites; Lax wel bij gewone links.',
		kind: 'enum',
		options: [
			{ value: 'Lax', label: 'Lax' },
			{ value: 'Strict', label: 'Strict' }
		],
		weakens: (o: string, n: string) => o === 'Strict' && n === 'Lax'
	},

	'login.passkey_required': {
		label: 'Passkey verplicht',
		help: 'Na je AD-wachtwoord is altijd een passkey nodig.',
		kind: 'bool',
		weakens: turnedOff
	},
	'login.passkey_only_on_trusted_device': {
		label: 'Alleen passkey op vertrouwd apparaat',
		help: 'Op een vertrouwd apparaat volstaat de passkey en is geen wachtwoord nodig.',
		kind: 'bool',
		weakens: turnedOn
	},
	'login.user_verification': {
		label: 'Gebruikersverificatie',
		help: 'Verplicht: de passkey vraagt altijd om Face ID, vingerafdruk of pincode.',
		kind: 'enum',
		options: [
			{ value: 'required', label: 'Verplicht' },
			{ value: 'preferred', label: 'Indien mogelijk' }
		],
		weakens: (o: string, n: string) => o === 'required' && n !== 'required'
	},
	'login.step_up_minutes': {
		label: 'Opnieuw bevestigen na',
		help: 'Wijzigingen in Instellingen vragen om je passkey als je die langer geleden gebruikte.',
		kind: 'int',
		min: 1,
		max: 60,
		unit: 'minuten',
		weakens: raised
	},

	'brute_force.login_per_ip_per_minute': {
		label: 'Pogingen per IP-adres',
		help: 'Maximaal aantal inlogpogingen per minuut vanaf één adres.',
		kind: 'int',
		min: 1,
		max: 1000,
		unit: 'per minuut',
		weakens: raised
	},
	'brute_force.login_per_account_per_minute': {
		label: 'Pogingen per account',
		help: 'Maximaal aantal inlogpogingen per minuut voor je account.',
		kind: 'int',
		min: 1,
		max: 1000,
		unit: 'per minuut',
		weakens: raised
	},
	'brute_force.lockout_threshold': {
		label: 'Blokkeren na',
		help: 'Aantal mislukte pogingen waarna inloggen tijdelijk wordt geblokkeerd.',
		kind: 'int',
		min: 1,
		max: 1000,
		unit: 'pogingen',
		weakens: raised
	},
	'brute_force.lockout_minutes': {
		label: 'Blokkeerduur',
		help: 'Hoe lang inloggen geblokkeerd blijft.',
		kind: 'int',
		min: 1,
		max: 1000,
		unit: 'minuten',
		weakens: lowered
	},

	'network.login_cidrs': {
		label: 'Inloggen toegestaan vanaf',
		help: 'Netwerken (CIDR) die mogen inloggen. Leeg = overal vandaan.',
		kind: 'cidrs',
		placeholder: '192.168.1.0/24',
		weakens: allowlistWidened
	},
	'network.admin_cidrs': {
		label: 'Instellingen toegestaan vanaf',
		help: 'Netwerken (CIDR) die Instellingen mogen openen. Leeg = overal vandaan.',
		kind: 'cidrs',
		placeholder: '192.168.1.0/24',
		weakens: allowlistWidened
	},
	'network.trusted_proxies': {
		label: 'Vertrouwde proxy’s',
		help: 'Proxy’s waarvan Keystone het X-Forwarded-For-adres gelooft. Staat er nog een reverse proxy vóór Keystone (bijv. voor https), voeg dan het adres toe waarmee die bij Keystone binnenkomt — vaak de Docker-gateway zoals 172.18.0.1/32. Dat adres zie je hieronder als jouw adres, en bij je sessies onder Apparaten.',
		kind: 'cidrs',
		placeholder: '10.0.0.2/32',
		weakens: added
	},

	'headers.hsts': {
		label: 'HSTS',
		help: 'Browsers gebruiken voortaan alleen https voor Keystone.',
		kind: 'bool',
		weakens: turnedOff
	},
	'headers.hsts_include_subdomains': {
		label: 'HSTS voor subdomeinen',
		help: 'Geldt ook voor alle subdomeinen van dit adres.',
		kind: 'bool',
		weakens: turnedOff
	},
	'headers.hsts_preload': {
		label: 'HSTS-preload',
		help: 'Staat opname in de preload-lijst van browsers toe. Alleen voor publieke domeinen.',
		kind: 'bool',
		weakens: turnedOff
	},
	'headers.frame_deny': {
		label: 'Insluiten blokkeren',
		help: 'Andere sites mogen Keystone niet in een frame tonen (beschermt tegen clickjacking).',
		kind: 'bool',
		weakens: turnedOff
	},
	'headers.hsts_max_age': {
		label: 'HSTS-duur',
		help: 'Hoe lang browsers de https-afspraak onthouden. 31536000 = een jaar.',
		kind: 'int',
		min: 0,
		max: 63072000,
		unit: 'seconden',
		weakens: lowered
	},
	'headers.csp_extra': {
		label: 'Extra CSP-regels',
		help: 'Wordt toegevoegd aan de Content-Security-Policy. Leeg laten tenzij je weet waarom.',
		kind: 'text',
		placeholder: "img-src 'self' https://cdn.example",
		weakens: (o: string, n: string) => n.trim() !== '' && n.trim() !== o.trim()
	},

	'audit.retention_days': {
		label: 'Activiteit bewaren',
		help: 'Hoe lang de activiteitenlog bewaard blijft. 0 = voor altijd.',
		kind: 'int',
		min: 0,
		max: 3650,
		unit: 'dagen',
		weakens: (o: number, n: number) => n > 0 && (o === 0 || n < o)
	},
	'audit.syslog_enabled': {
		label: 'Doorsturen naar syslog',
		help: 'Stuur elke gebeurtenis ook naar een syslog-server.',
		kind: 'bool',
		weakens: turnedOff
	},
	'audit.syslog_addr': {
		label: 'Syslog-server',
		help: 'Host en poort, bijvoorbeeld 192.168.1.10:514.',
		kind: 'text',
		placeholder: '192.168.1.10:514'
	},
	'audit.syslog_proto': {
		label: 'Syslog-protocol',
		help: 'TLS versleutelt het verkeer naar de syslog-server.',
		kind: 'enum',
		options: [
			{ value: 'udp', label: 'UDP' },
			{ value: 'tcp', label: 'TCP' },
			{ value: 'tls', label: 'TLS' }
		],
		weakens: (o: string, n: string) => o === 'tls' && n !== 'tls'
	}
};

/** The Beveiliging page, top to bottom. Herstel (codes, nood-account) follows separately. */
export interface SettingsGroup {
	id: string;
	title: string;
	paths: string[];
	foot?: string;
	/** Show "Jouw adres volgens Keystone" under the group (from GET /me). */
	showClientIP?: boolean;
}

export const securityGroups: SettingsGroup[] = [
	{
		id: 'sessies',
		title: 'Sessies',
		paths: [
			'session.duration_hours',
			'session.sliding',
			'session.idle_timeout_minutes',
			'session.trusted_device_days',
			'session.same_site'
		]
	},
	{
		id: 'inloggen',
		title: 'Inloggen',
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
		id: 'netwerk',
		title: 'Netwerk',
		paths: ['network.login_cidrs', 'network.admin_cidrs', 'network.trusted_proxies'],
		showClientIP: true
	},
	{
		id: 'headers',
		title: 'HTTP-headers',
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

/** Client-side check before saving (the server validates again): Dutch message or null. */
export function fieldError(meta: FieldMeta, v: unknown): string | null {
	if (meta.kind !== 'int') return null;
	if (typeof v !== 'number' || !Number.isInteger(v)) return 'Vul een geheel getal in.';
	if (v < meta.min! || v > meta.max!) return `Kies een waarde van ${meta.min} tot ${meta.max}.`;
	return null;
}
