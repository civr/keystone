import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import defaults from './fixtures/defaults.json';
import { saveSettings } from './settingsSave';
import { setPath, type Settings } from './settingsMeta';
import { configureStepUp } from './api';

const json = (status: number, body: unknown) =>
	new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

let fetchMock: ReturnType<typeof vi.fn>;
const saved = defaults as Settings;

beforeEach(() => {
	fetchMock = vi.fn();
	vi.stubGlobal('fetch', fetchMock);
	vi.stubGlobal('location', { pathname: '/instellingen/beveiliging', assign: vi.fn() });
	configureStepUp(null);
});
afterEach(() => vi.unstubAllGlobals());

const ask = (weakened = true, rpChange = true) => ({
	weakened: vi.fn(async () => weakened),
	rpChange: vi.fn(async () => rpChange)
});

describe('saveSettings', () => {
	it('PUTs the full object without asking when nothing weakens', async () => {
		const draft = setPath(saved, 'session.duration_hours', 24);
		fetchMock.mockResolvedValueOnce(json(200, draft));
		const a = ask();
		await expect(saveSettings(saved, draft, a)).resolves.toEqual({ status: 'saved', settings: draft });
		expect(a.weakened).not.toHaveBeenCalled();
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe('/api/admin/settings');
		expect(init.method).toBe('PUT');
		expect(JSON.parse(init.body)).toEqual(draft);
	});

	it('asks before a weakening change and does not save when cancelled', async () => {
		const draft = setPath(saved, 'login.passkey_required', false);
		const a = ask(false);
		await expect(saveSettings(saved, draft, a)).resolves.toEqual({ status: 'cancelled' });
		expect(a.weakened).toHaveBeenCalledWith(['login.passkey_required']);
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it('saves a weakening change once confirmed', async () => {
		const draft = setPath(saved, 'headers.hsts', false);
		fetchMock.mockResolvedValueOnce(json(200, draft));
		await expect(saveSettings(saved, draft, ask(true))).resolves.toMatchObject({ status: 'saved' });
	});

	it('returns the field of a 400 invalid answer', async () => {
		const draft = setPath(saved, 'network.admin_cidrs', ['10.0.0.0/8']);
		fetchMock.mockResolvedValueOnce(
			json(400, { error: 'invalid', field: 'network.admin_cidrs', message: 'dit zou je eigen adres buitensluiten' })
		);
		await expect(saveSettings(saved, draft, ask())).resolves.toEqual({
			status: 'invalid',
			field: 'network.admin_cidrs',
			message: 'dit zou je eigen adres buitensluiten'
		});
	});

	it('retries with ?confirm=1 after a passkeys_rp_change warning is confirmed', async () => {
		const draft = setPath(saved, 'public_url', 'https://id.example.org');
		fetchMock
			.mockResolvedValueOnce(json(409, { error: 'confirm_required', warning: 'passkeys_rp_change' }))
			.mockResolvedValueOnce(json(200, draft));
		const a = ask();
		await expect(saveSettings(saved, draft, a)).resolves.toMatchObject({ status: 'saved' });
		expect(a.rpChange).toHaveBeenCalledOnce();
		expect(fetchMock.mock.calls[1][0]).toBe('/api/admin/settings?confirm=1');
	});

	it('stops when the passkeys_rp_change warning is declined', async () => {
		const draft = setPath(saved, 'public_url', 'https://id.example.org');
		fetchMock.mockResolvedValueOnce(json(409, { error: 'confirm_required', warning: 'passkeys_rp_change' }));
		await expect(saveSettings(saved, draft, ask(true, false))).resolves.toEqual({ status: 'cancelled' });
		expect(fetchMock).toHaveBeenCalledOnce();
	});

	it('rethrows other errors', async () => {
		const draft = setPath(saved, 'session.duration_hours', 24);
		fetchMock.mockResolvedValueOnce(json(503, { error: 'unavailable', message: 'Even niet' }));
		await expect(saveSettings(saved, draft, ask())).rejects.toMatchObject({ code: 'unavailable' });
	});
});
