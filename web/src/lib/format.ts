/** Display text for Settings: times, devices, setting values, audit entries. */
import { passkeyName } from './webauthn';
import { settingsMeta, type FieldMeta } from './settingsMeta';

const dateFmt = new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'long', year: 'numeric' });
const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** "just now", "5 minutes ago", "yesterday", … or a date after a week. */
export function relativeTime(iso: string | null | undefined, now: Date = new Date()): string {
	if (!iso) return 'never';
	const at = new Date(iso);
	const s = Math.max(0, (now.getTime() - at.getTime()) / 1000);
	if (s < 45) return 'just now';
	if (s < 3600) return plural(Math.max(1, Math.round(s / 60)), 'minute', 'minutes') + ' ago';
	if (s < 86400) return plural(Math.round(s / 3600), 'hour', 'hours') + ' ago';
	const days = Math.floor(s / 86400);
	if (days === 1) return 'yesterday';
	if (days < 7) return `${days} days ago`;
	return dateFmt.format(at);
}

/** "Safari on iPhone" from a User-Agent string. */
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
	if (browser && device) return `${browser} on ${device}`;
	return browser || device || 'Unknown device';
}

/** A setting value as the user reads it, e.g. "On", "720 hours", "When possible". */
export function formatValue(meta: FieldMeta | undefined, v: unknown): string {
	if (!meta) return typeof v === 'string' ? v : JSON.stringify(v);
	switch (meta.kind) {
		case 'bool':
			return v ? 'On' : 'Off';
		case 'int':
			return meta.unit ? `${v} ${meta.unit}` : String(v);
		case 'enum':
			return meta.options?.find((o) => o.value === v)?.label ?? String(v);
		case 'cidrs':
			return Array.isArray(v) && v.length ? v.join(', ') : 'empty (anywhere)';
		default:
			return typeof v === 'string' && v !== '' ? v : 'empty';
	}
}

const ACTIONS: Record<string, string> = {
	'login.success': 'Signed in',
	'login.failure': 'Sign-in failed',
	'login.locked': 'Sign-in locked',
	'login.not_allowed': 'Other account refused',
	logout: 'Signed out',
	stepup: 'Confirmed',
	'recovery.used': 'Recovery code used',
	'recovery.regenerated': 'New recovery codes made',
	'emergency.used': 'Emergency account used',
	'emergency.changed': 'Emergency account changed',
	'settings.changed': 'Settings changed',
	'branding.changed': 'Branding changed',
	'vault.item_created': 'Vault item added',
	'vault.item_updated': 'Vault item changed',
	'vault.item_deleted': 'Vault item deleted',
	'user.created': 'User added',
	'user.updated': 'User changed',
	'user.deleted': 'User deleted',
	'account.password_changed': 'Password changed',
	'vault.provider_added': 'Credential provider added',
	'vault.provider_removed': 'Credential provider removed',
	'totp.enabled': 'Authenticator added',
	'totp.disabled': 'Authenticator removed',
	'totp.reset': 'Authenticator reset by an admin',
	'role.created': 'Role added',
	'role.updated': 'Role changed',
	'role.deleted': 'Role deleted',
	'user.roles_changed': 'Roles changed',
	'group.created': 'Group added',
	'group.updated': 'Group changed',
	'group.deleted': 'Group deleted',
	'group.roles_changed': 'Group roles changed',
	'settings.network_reset': 'Network limits cleared',
	'ad.changed': 'Active Directory changed',
	'session.revoked': 'Session signed out',
	'passkey.registered': 'Passkey added',
	'passkey.renamed': 'Passkey renamed',
	'passkey.removed': 'Passkey removed',
	'app.created': 'App added',
	'app.updated': 'App changed',
	'app.deleted': 'App deleted',
	'app.reordered': 'Apps reordered',
	'app.secret_created': 'New app secret made',
	'app.access': 'App opened',
	'keys.rotated': 'Key renewed',
	'vault.updated': 'Vault changed',
	'vault.revealed': 'Vault password used',
	'ldap.bind_failed': 'LDAP sign-in failed',
	'gate.denied': 'Gate refused access',
	'audit.exported': 'Activity exported',
	'setup.unlocked': 'Setup unlocked',
	'setup.unlock_failed': 'Wrong setup code',
	'setup.domain': 'Setup: domain set',
	'setup.ad': 'Setup: Active Directory set',
	'setup.owner': 'Setup: owner chosen',
	'setup.owner_failed': 'Setup: owner not recognised',
	'setup.account': 'Setup: account created',
	'setup.passkey': 'Setup: passkey registered',
	'setup.passkey_skipped': 'Setup: passkey skipped',
	'setup.emergency': 'Setup: emergency account set',
	'setup.completed': 'Setup completed'
};

export function auditLabel(action: string): string {
	return ACTIONS[action] ?? action;
}

const KEYS: Record<string, string> = {
	username: 'user',
	method: 'method',
	reason: 'reason',
	name: 'name',
	remaining: 'left',
	domain: 'domain',
	public_url: 'address',
	url: 'server',
	rows: 'rows',
	recovery_codes: 'recovery codes',
	after: 'after',
	title: 'item',
	app: 'app',
	role: 'role'
};
const VALUES: Record<string, string> = {
	password: 'password',
	passkey: 'passkey',
	passkey_register: 'new passkey',
	trusted_device: 'trusted device (passkey)',
	emergency: 'emergency account',
	recovery: 'recovery code',
	stepup: 'confirmation',
	launch: 'signed in for you',
	proxy: 'reverse proxy',
	totp: 'authenticator code',
	ad_unavailable: 'Active Directory unreachable',
	ad_error: 'Active Directory error'
};

/** Text for a known detail value (e.g. a login method), else undefined. */
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
