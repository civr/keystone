import { describe, expect, it } from 'vitest';
import { auditLabel, auditSummary, auditValueLabel, describeUA, formatValue, relativeTime } from './format';
import { settingsMeta } from './settingsMeta';
import auditFixture from './fixtures/audit-actions.json';

describe('relativeTime', () => {
	const now = new Date('2026-10-21T12:00:00Z');
	const ago = (s: number) => new Date(now.getTime() - s * 1000).toISOString();
	it('reads naturally', () => {
		expect(relativeTime(ago(10), now)).toBe('just now');
		expect(relativeTime(ago(60), now)).toBe('1 minute ago');
		expect(relativeTime(ago(5 * 60), now)).toBe('5 minutes ago');
		expect(relativeTime(ago(3600), now)).toBe('1 hour ago');
		expect(relativeTime(ago(26 * 3600), now)).toBe('yesterday');
		expect(relativeTime(ago(3 * 86400), now)).toBe('3 days ago');
	});
	it('falls back to a date after a week, and handles null', () => {
		expect(relativeTime(ago(30 * 86400), now)).toMatch(/September/);
		expect(relativeTime(null, now)).toBe('never');
	});
});

describe('describeUA', () => {
	it('names browser and device', () => {
		expect(
			describeUA(
				'Mozilla/5.0 (iPhone; CPU iPhone OS 19_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/19.0 Mobile/15E148 Safari/604.1'
			)
		).toBe('Safari on iPhone');
		expect(
			describeUA(
				'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36 Edg/141.0'
			)
		).toBe('Edge on Windows PC');
		expect(describeUA('Mozilla/5.0 (Macintosh; Intel Mac OS X 15_0; rv:140.0) Gecko/20100101 Firefox/140.0')).toBe(
			'Firefox on Mac'
		);
		expect(describeUA('Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36')).toBe(
			'Chrome on Linux'
		);
		expect(describeUA('')).toBe('Unknown device');
		expect(describeUA('curl/8.0')).toBe('Unknown device');
	});
});

describe('formatValue', () => {
	it('formats each kind', () => {
		expect(formatValue(settingsMeta['login.passkey_required'], true)).toBe('On');
		expect(formatValue(settingsMeta['login.passkey_required'], false)).toBe('Off');
		expect(formatValue(settingsMeta['session.duration_hours'], 720)).toBe('720 hours');
		expect(formatValue(settingsMeta['login.user_verification'], 'preferred')).toBe('When possible');
		expect(formatValue(settingsMeta['network.login_cidrs'], [])).toBe('empty (anywhere)');
		expect(formatValue(settingsMeta['network.login_cidrs'], ['10.0.0.0/8', 'fd00::/8'])).toBe('10.0.0.0/8, fd00::/8');
		expect(formatValue(settingsMeta['headers.csp_extra'], '')).toBe('empty');
	});
});

describe('audit text', () => {
	it('labels known actions and passes unknown ones through', () => {
		expect(auditLabel('login.success')).toBe('Signed in');
		expect(auditLabel('settings.changed')).toBe('Settings changed');
		expect(auditLabel('something.new')).toBe('something.new');
	});
	it('summarizes settings changes with labels', () => {
		const s = auditSummary({
			action: 'settings.changed',
			detail: { changes: [{ path: 'session.duration_hours', old: 720, new: 24 }, { path: 'x.unknown', old: 1, new: 2 }] }
		});
		expect(s).toEqual(['Session length: 720 hours → 24 hours', 'x.unknown: 1 → 2']);
	});
	it('summarizes other details briefly', () => {
		expect(auditSummary({ action: 'login.failure', detail: { username: 'thijs', method: 'password' } })).toEqual([
			'user thijs',
			'method password'
		]);
		expect(auditSummary({ action: 'logout', detail: {} })).toEqual([]);
		expect(auditSummary({ action: 'logout', detail: null })).toEqual([]);
	});
});

describe('audit labels', () => {
	it.each(auditFixture.actions)('labels %s', (action) => {
		expect(auditLabel(action)).not.toBe(action);
	});
	it.each(auditFixture.methods)('names login method %s', (method) => {
		const label = auditValueLabel(method);
		expect(label).toBeTruthy();
		expect(auditSummary({ action: 'login.success', detail: { method } })).toEqual([`method ${label}`]);
	});
});
