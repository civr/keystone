import { describe, expect, it } from 'vitest';
import defaults from './fixtures/defaults.json';
import {
	settingsMeta,
	securityGroups,
	leafPaths,
	getPath,
	setPath,
	changedPaths,
	weakenedPaths,
	fieldError,
	type Settings
} from './settingsMeta';

const clone = (): Settings => structuredClone(defaults) as Settings;

function weakens(path: string, from: unknown, to: unknown): boolean {
	const meta = settingsMeta[path];
	expect(meta, path).toBeDefined();
	return meta.weakens?.(from, to) ?? false;
}

describe('settingsMeta coverage', () => {
	it('every Settings field from Defaults() JSON has meta', () => {
		const paths = leafPaths(defaults);
		expect(paths.length).toBeGreaterThan(20);
		for (const p of paths) expect(settingsMeta[p], `missing meta for ${p}`).toBeDefined();
	});

	it('has no meta for fields that do not exist', () => {
		const paths = new Set(leafPaths(defaults));
		for (const p of Object.keys(settingsMeta)) expect(paths.has(p), `stale meta ${p}`).toBe(true);
	});

	it('kinds match the default values and defaults are in range', () => {
		for (const [path, m] of Object.entries(settingsMeta)) {
			const v = getPath(defaults, path);
			expect(m.label.trim(), path).not.toBe('');
			expect(m.help.trim(), path).not.toBe('');
			if (m.kind === 'bool') expect(typeof v, path).toBe('boolean');
			if (m.kind === 'cidrs') expect(Array.isArray(v), path).toBe(true);
			if (m.kind === 'text') expect(typeof v, path).toBe('string');
			if (m.kind === 'enum') expect(m.options?.map((o) => o.value), path).toContain(v);
			if (m.kind === 'int') {
				expect(Number.isInteger(v), path).toBe(true);
				expect(m.min, path).toBeTypeOf('number');
				expect(m.max, path).toBeTypeOf('number');
				expect(v as number, path).toBeGreaterThanOrEqual(m.min!);
				expect(v as number, path).toBeLessThanOrEqual(m.max!);
			}
		}
	});

	it('Beveiliging groups list every security field exactly once', () => {
		const grouped = securityGroups.flatMap((g) => g.paths);
		expect(new Set(grouped).size).toBe(grouped.length);
		const general = ['domain', 'public_url'];
		const expected = Object.keys(settingsMeta).filter((p) => !general.includes(p));
		expect([...grouped].sort()).toEqual(expected.sort());
		expect(securityGroups.map((g) => g.title)).toEqual([
			'Sessies',
			'Inloggen',
			'Brute-force',
			'Netwerk',
			'HTTP-headers',
			'Audit'
		]);
	});
});

describe('weakens', () => {
	it('passkey_required true→false', () => {
		expect(weakens('login.passkey_required', true, false)).toBe(true);
		expect(weakens('login.passkey_required', false, true)).toBe(false);
	});
	it('hsts true→false', () => {
		expect(weakens('headers.hsts', true, false)).toBe(true);
		expect(weakens('headers.hsts', false, true)).toBe(false);
	});
	it('session duration up', () => {
		expect(weakens('session.duration_hours', 720, 1000)).toBe(true);
		expect(weakens('session.duration_hours', 720, 24)).toBe(false);
	});
	it('lockout threshold up', () => {
		expect(weakens('brute_force.lockout_threshold', 10, 20)).toBe(true);
		expect(weakens('brute_force.lockout_threshold', 10, 5)).toBe(false);
	});
	it('user_verification required→preferred', () => {
		expect(weakens('login.user_verification', 'required', 'preferred')).toBe(true);
		expect(weakens('login.user_verification', 'preferred', 'required')).toBe(false);
	});
	it('network allowlists emptied or widened', () => {
		expect(weakens('network.admin_cidrs', ['10.0.0.0/8'], [])).toBe(true);
		expect(weakens('network.login_cidrs', ['10.0.0.0/8'], [])).toBe(true);
		expect(weakens('network.login_cidrs', ['10.0.0.0/8'], ['10.0.0.0/8', '0.0.0.0/0'])).toBe(true);
		expect(weakens('network.login_cidrs', [], ['10.0.0.0/8'])).toBe(false);
		expect(weakens('network.login_cidrs', ['10.0.0.0/8', '192.168.0.0/16'], ['10.0.0.0/8'])).toBe(false);
		expect(weakens('network.trusted_proxies', [], ['10.0.0.1/32'])).toBe(true);
		expect(weakens('network.trusted_proxies', ['10.0.0.1/32'], [])).toBe(false);
	});
	it('frame_deny true→false', () => {
		expect(weakens('headers.frame_deny', true, false)).toBe(true);
		expect(weakens('headers.frame_deny', false, true)).toBe(false);
	});
	it('brute-force rate limits raised', () => {
		expect(weakens('brute_force.login_per_ip_per_minute', 10, 30)).toBe(true);
		expect(weakens('brute_force.login_per_account_per_minute', 5, 6)).toBe(true);
		expect(weakens('brute_force.login_per_ip_per_minute', 10, 5)).toBe(false);
	});
	it('audit retention lowered (0 = keep forever)', () => {
		expect(weakens('audit.retention_days', 365, 30)).toBe(true);
		expect(weakens('audit.retention_days', 0, 30)).toBe(true);
		expect(weakens('audit.retention_days', 30, 0)).toBe(false);
		expect(weakens('audit.retention_days', 30, 365)).toBe(false);
	});
	it('trusted device days raised', () => {
		expect(weakens('session.trusted_device_days', 90, 180)).toBe(true);
		expect(weakens('session.trusted_device_days', 90, 30)).toBe(false);
	});
	it('step-up window raised', () => {
		expect(weakens('login.step_up_minutes', 5, 15)).toBe(true);
		expect(weakens('login.step_up_minutes', 5, 1)).toBe(false);
	});
	it('idle timeout switched off or raised (0 = off)', () => {
		expect(weakens('session.idle_timeout_minutes', 30, 0)).toBe(true);
		expect(weakens('session.idle_timeout_minutes', 30, 60)).toBe(true);
		expect(weakens('session.idle_timeout_minutes', 0, 30)).toBe(false);
	});
	it('fields that cannot weaken security have no weakens', () => {
		expect(settingsMeta['domain'].weakens).toBeUndefined();
		expect(settingsMeta['audit.syslog_addr'].weakens).toBeUndefined();
	});
});

describe('path helpers', () => {
	it('getPath / setPath work on nested settings without mutating the input', () => {
		const s = clone();
		const next = setPath(s, 'login.passkey_required', false);
		expect(getPath(next, 'login.passkey_required')).toBe(false);
		expect(getPath(s, 'login.passkey_required')).toBe(true);
		expect(next.session).toBe(s.session); // untouched branches are shared
	});

	it('changedPaths compares values, including lists', () => {
		const a = clone();
		let b = setPath(a, 'session.duration_hours', 24);
		b = setPath(b, 'network.login_cidrs', ['10.0.0.0/8']);
		expect(changedPaths(a, b)).toEqual(['session.duration_hours', 'network.login_cidrs']);
		expect(changedPaths(a, structuredClone(a))).toEqual([]);
	});

	it('weakenedPaths lists only the changes that lower security', () => {
		const a = clone();
		let b = setPath(a, 'session.duration_hours', 24); // stronger
		b = setPath(b, 'headers.hsts', false); // weaker
		b = setPath(b, 'login.step_up_minutes', 10); // weaker
		expect(weakenedPaths(a, b)).toEqual(['login.step_up_minutes', 'headers.hsts']);
	});
});

describe('fieldError', () => {
	it('checks whole numbers within range', () => {
		const m = settingsMeta['session.duration_hours'];
		expect(fieldError(m, 720)).toBeNull();
		expect(fieldError(m, 0)).toBe('Kies een waarde van 1 tot 8760.');
		expect(fieldError(m, 9000)).toBe('Kies een waarde van 1 tot 8760.');
		expect(fieldError(m, 1.5)).toBe('Vul een geheel getal in.');
		expect(fieldError(m, NaN)).toBe('Vul een geheel getal in.');
	});
	it('accepts other kinds as they are', () => {
		expect(fieldError(settingsMeta['headers.hsts'], false)).toBeNull();
		expect(fieldError(settingsMeta['network.login_cidrs'], ['x'])).toBeNull();
	});
});
