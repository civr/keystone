/** Dutch display text for Instellingen: times, devices, setting values, audit entries. */
import { passkeyName } from './webauthn';
import { settingsMeta, type FieldMeta } from './settingsMeta';

const dateFmt = new Intl.DateTimeFormat('nl-NL', { day: 'numeric', month: 'long', year: 'numeric' });
const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** "zojuist", "5 minuten geleden", "gisteren", … or a date after a week. */
export function relativeTime(iso: string | null | undefined, now: Date = new Date()): string {
	if (!iso) return 'nooit';
	const at = new Date(iso);
	const s = Math.max(0, (now.getTime() - at.getTime()) / 1000);
	if (s < 45) return 'zojuist';
	if (s < 3600) return plural(Math.max(1, Math.round(s / 60)), 'minuut', 'minuten') + ' geleden';
	if (s < 86400) return plural(Math.round(s / 3600), 'uur', 'uur') + ' geleden';
	const days = Math.floor(s / 86400);
	if (days === 1) return 'gisteren';
	if (days < 7) return `${days} dagen geleden`;
	return dateFmt.format(at);
}

/** "Safari op iPhone" from a User-Agent string. */
export function describeUA(ua: string): string {
	const browser = /Edg\//.test(ua)
		? 'Edge'
		: /Firefox\//.test(ua)
			? 'Firefox'
			: /Chrome\//.test(ua)
				? 'Chrome'
				: /Version\/.*Safari\//.test(ua)
					? 'Safari'
					: '';
	const name = passkeyName(ua);
	const device = name === 'Passkey' ? '' : name;
	if (browser && device) return `${browser} op ${device}`;
	return browser || device || 'Onbekend apparaat';
}

/** A setting value as the user reads it, e.g. "Aan", "720 uur", "Indien mogelijk". */
export function formatValue(meta: FieldMeta | undefined, v: unknown): string {
	if (!meta) return typeof v === 'string' ? v : JSON.stringify(v);
	switch (meta.kind) {
		case 'bool':
			return v ? 'Aan' : 'Uit';
		case 'int':
			return meta.unit ? `${v} ${meta.unit}` : String(v);
		case 'enum':
			return meta.options?.find((o) => o.value === v)?.label ?? String(v);
		case 'cidrs':
			return Array.isArray(v) && v.length ? v.join(', ') : 'leeg (overal vandaan)';
		default:
			return typeof v === 'string' && v !== '' ? v : 'leeg';
	}
}

const ACTIONS: Record<string, string> = {
	'login.success': 'Ingelogd',
	'login.failure': 'Inloggen mislukt',
	'login.locked': 'Inloggen geblokkeerd',
	'login.not_allowed': 'Ander account geweigerd',
	logout: 'Uitgelogd',
	stepup: 'Bevestigd met passkey',
	'recovery.used': 'Herstelcode gebruikt',
	'recovery.regenerated': 'Nieuwe herstelcodes gemaakt',
	'emergency.used': 'Nood-account gebruikt',
	'emergency.changed': 'Nood-account gewijzigd',
	'settings.changed': 'Instellingen gewijzigd',
	'branding.changed': 'Huisstijl gewijzigd',
	'settings.network_reset': 'Netwerkbeperkingen gewist',
	'ad.changed': 'Active Directory gewijzigd',
	'session.revoked': 'Sessie afgemeld',
	'passkey.registered': 'Passkey toegevoegd',
	'passkey.renamed': 'Passkey hernoemd',
	'passkey.removed': 'Passkey verwijderd',
	'app.created': 'App toegevoegd',
	'app.updated': 'App gewijzigd',
	'app.deleted': 'App verwijderd',
	'app.reordered': 'Apps opnieuw geordend',
	'app.secret_created': 'Nieuw app-geheim gemaakt',
	'app.access': 'App geopend',
	'keys.rotated': 'Sleutel vernieuwd',
	'vault.updated': 'Kluis gewijzigd',
	'vault.revealed': 'Wachtwoord uit kluis gebruikt',
	'ldap.bind_failed': 'LDAP-inlog mislukt',
	'gate.denied': 'Poortwachter weigerde toegang',
	'audit.exported': 'Activiteit geëxporteerd',
	'setup.unlocked': 'Installatie ontgrendeld',
	'setup.unlock_failed': 'Verkeerde setupcode',
	'setup.domain': 'Installatie: domein ingesteld',
	'setup.ad': 'Installatie: Active Directory ingesteld',
	'setup.owner': 'Installatie: eigenaar gekozen',
	'setup.owner_failed': 'Installatie: eigenaar niet herkend',
	'setup.account': 'Installatie: account aangemaakt',
	'setup.passkey': 'Installatie: passkey geregistreerd',
	'setup.passkey_skipped': 'Installatie: passkey overgeslagen',
	'setup.emergency': 'Installatie: nood-account ingesteld',
	'setup.completed': 'Installatie voltooid'
};

export function auditLabel(action: string): string {
	return ACTIONS[action] ?? action;
}

const KEYS: Record<string, string> = {
	username: 'gebruiker',
	method: 'methode',
	reason: 'reden',
	name: 'naam',
	remaining: 'over',
	domain: 'domein',
	public_url: 'adres',
	url: 'server',
	rows: 'regels',
	recovery_codes: 'herstelcodes',
	after: 'na'
};
const VALUES: Record<string, string> = {
	password: 'wachtwoord',
	passkey: 'passkey',
	passkey_register: 'nieuwe passkey',
	trusted_device: 'vertrouwd apparaat (passkey)',
	emergency: 'nood-account',
	recovery: 'herstelcode',
	stepup: 'bevestiging',
	ad_unavailable: 'AD niet bereikbaar',
	ad_error: 'AD-fout'
};

/** Dutch text for a known detail value (e.g. a login method), else undefined. */
export function auditValueLabel(v: string): string | undefined {
	return VALUES[v];
}

interface Change {
	path: string;
	old: unknown;
	new: unknown;
}

/** Short lines describing an audit entry's detail. */
export function auditSummary(e: { action: string; detail: unknown }): string[] {
	const d = (e.detail && typeof e.detail === 'object' ? e.detail : {}) as Record<string, unknown>;
	if (Array.isArray(d.changes)) {
		return (d.changes as Change[]).map((c) => {
			const m = settingsMeta[c.path];
			return `${m?.label ?? c.path}: ${formatValue(m, c.old)} → ${formatValue(m, c.new)}`;
		});
	}
	return Object.entries(d)
		.filter(([, v]) => typeof v === 'string' || typeof v === 'number')
		.map(([k, v]) => `${KEYS[k] ?? k} ${VALUES[String(v)] ?? v}`);
}
