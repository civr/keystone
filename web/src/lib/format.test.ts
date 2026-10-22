import { describe, expect, it } from 'vitest';
import { auditLabel, auditSummary, auditValueLabel, describeUA, formatValue, relativeTime } from './format';
import { settingsMeta } from './settingsMeta';
import auditFixture from './fixtures/audit-actions.json';

describe('relativeTime', () => {
	const now = new Date('2026-10-21T12:00:00Z');
	const ago = (s: number) => new Date(now.getTime() - s * 1000).toISOString();
	it('reads naturally in Dutch', () => {
		expect(relativeTime(ago(10), now)).toBe('zojuist');
		expect(relativeTime(ago(60), now)).toBe('1 minuut geleden');
		expect(relativeTime(ago(5 * 60), now)).toBe('5 minuten geleden');
		expect(relativeTime(ago(3600), now)).toBe('1 uur geleden');
		expect(relativeTime(ago(26 * 3600), now)).toBe('gisteren');
		expect(relativeTime(ago(3 * 86400), now)).toBe('3 dagen geleden');
	});
	it('falls back to a date after a week, and handles null', () => {
		expect(relativeTime(ago(30 * 86400), now)).toMatch(/september/);
		expect(relativeTime(null, now)).toBe('nooit');
	});
});

describe('describeUA', () => {
	it('names browser and device', () => {
		expect(
			describeUA(
				'Mozilla/5.0 (iPhone; CPU iPhone OS 19_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/19.0 Mobile/15E148 Safari/604.1'
			)
		).toBe('Safari op iPhone');
		expect(
			describeUA(
				'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36 Edg/141.0'
			)
		).toBe('Edge op Windows-pc');
		expect(describeUA('Mozilla/5.0 (Macintosh; Intel Mac OS X 15_0; rv:140.0) Gecko/20100101 Firefox/140.0')).toBe(
			'Firefox op Mac'
		);
		expect(describeUA('Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36')).toBe(
			'Chrome op Linux'
		);
		expect(describeUA('')).toBe('Onbekend apparaat');
		expect(describeUA('curl/8.0')).toBe('Onbekend apparaat');
	});
});

describe('formatValue', () => {
	it('formats each kind', () => {
		expect(formatValue(settingsMeta['login.passkey_required'], true)).toBe('Aan');
		expect(formatValue(settingsMeta['login.passkey_required'], false)).toBe('Uit');
		expect(formatValue(settingsMeta['session.duration_hours'], 720)).toBe('720 uur');
		expect(formatValue(settingsMeta['login.user_verification'], 'preferred')).toBe('Indien mogelijk');
		expect(formatValue(settingsMeta['network.login_cidrs'], [])).toBe('leeg (overal vandaan)');
		expect(formatValue(settingsMeta['network.login_cidrs'], ['10.0.0.0/8', 'fd00::/8'])).toBe('10.0.0.0/8, fd00::/8');
		expect(formatValue(settingsMeta['headers.csp_extra'], '')).toBe('leeg');
	});
});

describe('audit text', () => {
	it('labels known actions and passes unknown ones through', () => {
		expect(auditLabel('login.success')).toBe('Ingelogd');
		expect(auditLabel('settings.changed')).toBe('Instellingen gewijzigd');
		expect(auditLabel('something.new')).toBe('something.new');
	});
	it('summarizes settings changes with labels', () => {
		const s = auditSummary({
			action: 'settings.changed',
			detail: { changes: [{ path: 'session.duration_hours', old: 720, new: 24 }, { path: 'x.unknown', old: 1, new: 2 }] }
		});
		expect(s).toEqual(['Sessieduur: 720 uur → 24 uur', 'x.unknown: 1 → 2']);
	});
	it('summarizes other details briefly', () => {
		expect(auditSummary({ action: 'login.failure', detail: { username: 'thijs', method: 'password' } })).toEqual([
			'gebruiker thijs',
			'methode wachtwoord'
		]);
		expect(auditSummary({ action: 'logout', detail: {} })).toEqual([]);
		expect(auditSummary({ action: 'logout', detail: null })).toEqual([]);
	});
});

describe('audit labels', () => {
	it.each(auditFixture.actions)('labels %s in Dutch', (action) => {
		expect(auditLabel(action)).not.toBe(action);
	});
	it.each(auditFixture.methods)('names login method %s in Dutch', (method) => {
		const label = auditValueLabel(method);
		expect(label).toBeTruthy();
		expect(auditSummary({ action: 'login.success', detail: { method } })).toEqual([`methode ${label}`]);
	});
});
