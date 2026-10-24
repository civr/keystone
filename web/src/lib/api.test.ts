import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api, ApiError, configureStepUp } from './api';

const json = (status: number, body: unknown) =>
	new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

let fetchMock: ReturnType<typeof vi.fn>;
let assign: ReturnType<typeof vi.fn>;

beforeEach(() => {
	fetchMock = vi.fn();
	assign = vi.fn();
	vi.stubGlobal('fetch', fetchMock);
	vi.stubGlobal('location', { pathname: '/', assign });
	configureStepUp(null);
});
afterEach(() => vi.unstubAllGlobals());

/** The ApiError a promise rejects with; fails the test if it resolves. */
const rejection = (p: Promise<unknown>) =>
	p.then(
		() => {
			throw new Error('expected the request to fail');
		},
		(e: unknown) => e as ApiError
	);

describe('api.get', () => {
	it('fetches same-origin JSON', async () => {
		fetchMock.mockResolvedValueOnce(json(200, { username: 'thijs' }));
		await expect(api.get('/me')).resolves.toEqual({ username: 'thijs' });
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe('/api/me');
		expect(init.credentials).toBe('same-origin');
		expect(init.method).toBe('GET');
	});

	it('throws ApiError with code and message', async () => {
		fetchMock.mockResolvedValueOnce(json(404, { error: 'not_found', message: 'Not found' }));
		const err = await rejection(api.get('/apps/x'));
		expect(err).toBeInstanceOf(ApiError);
		expect(err.code).toBe('not_found');
		expect(err.message).toBe('Not found');
		expect(err.status).toBe(404);
	});

	it('maps a non-JSON failure to a generic error', async () => {
		fetchMock.mockResolvedValueOnce(new Response('bad gateway', { status: 502 }));
		const err = await rejection(api.get('/me'));
		expect(err).toBeInstanceOf(ApiError);
		expect(err.code).toBe('http_502');
		expect(err.message).toMatch(/Keystone/);
	});

	it('maps a network failure to an offline error', async () => {
		fetchMock.mockRejectedValueOnce(new TypeError('Failed to fetch'));
		const err = await rejection(api.get('/me'));
		expect(err).toBeInstanceOf(ApiError);
		expect(err.code).toBe('network');
	});

	it('redirects to /login on 401', async () => {
		fetchMock.mockResolvedValueOnce(json(401, { error: 'unauthenticated' }));
		const err = await rejection(api.get('/me'));
		expect(err.code).toBe('unauthenticated');
		expect(assign).toHaveBeenCalledWith('/login');
	});

	it('does not redirect when already on /login', async () => {
		vi.stubGlobal('location', { pathname: '/login', assign });
		fetchMock.mockResolvedValueOnce(json(401, { error: 'unauthenticated' }));
		await rejection(api.get('/me'));
		expect(assign).not.toHaveBeenCalled();
	});

	it('does not redirect from the setup wizard (wrong setup code is a 401)', async () => {
		vi.stubGlobal('location', { pathname: '/setup', assign });
		fetchMock.mockResolvedValueOnce(json(401, { error: 'bad_code', message: 'That setup code is not right.' }));
		const err = await rejection(api.send('POST', '/setup/unlock', { code: 'x' }));
		expect(err.message).toBe('That setup code is not right.');
		expect(assign).not.toHaveBeenCalled();
	});

	it('exposes extra error fields as data', async () => {
		fetchMock.mockResolvedValueOnce(
			json(503, { error: 'ad_unavailable', message: 'Active Directory cannot be reached.', emergency: true })
		);
		const err = await rejection(api.send('POST', '/auth/password', {}));
		expect(err.data.emergency).toBe(true);
		fetchMock.mockResolvedValueOnce(json(429, { error: 'locked', message: 'x', retry_after: 900 }));
		expect((await rejection(api.send('POST', '/auth/password', {}))).data.retry_after).toBe(900);
	});
});

describe('api.send', () => {
	it('sends JSON with Content-Type', async () => {
		fetchMock.mockResolvedValueOnce(json(200, { ok: true }));
		await api.send('POST', '/apps', { name: 'Grafana' });
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe('/api/apps');
		expect(init.method).toBe('POST');
		expect(init.headers['Content-Type']).toBe('application/json');
		expect(init.body).toBe('{"name":"Grafana"}');
		expect(init.credentials).toBe('same-origin');
	});

	it('resolves undefined for 204 No Content', async () => {
		fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
		await expect(api.send('DELETE', '/apps/1')).resolves.toBeUndefined();
	});

	it('runs step-up and retries once on stepup_required', async () => {
		const stepUp = vi.fn().mockResolvedValue(undefined);
		configureStepUp(stepUp);
		fetchMock
			.mockResolvedValueOnce(json(403, { error: 'stepup_required', message: 'Bevestig' }))
			.mockResolvedValueOnce(json(200, { id: 'a1' }));
		await expect(api.send('PUT', '/apps/a1', { name: 'X' })).resolves.toEqual({ id: 'a1' });
		expect(stepUp).toHaveBeenCalledTimes(1);
		expect(fetchMock).toHaveBeenCalledTimes(2);
		expect(fetchMock.mock.calls[1][1].body).toBe('{"name":"X"}');
	});

	it('gives up after one retry', async () => {
		const stepUp = vi.fn().mockResolvedValue(undefined);
		configureStepUp(stepUp);
		fetchMock
			.mockResolvedValueOnce(json(403, { error: 'stepup_required', message: 'Bevestig' }))
			.mockResolvedValueOnce(json(403, { error: 'stepup_required', message: 'Bevestig' }));
		const err = await rejection(api.send('PUT', '/apps/a1', {}));
		expect(err.code).toBe('stepup_required');
		expect(stepUp).toHaveBeenCalledTimes(1);
		expect(fetchMock).toHaveBeenCalledTimes(2);
	});

	it('does not retry when the step-up ceremony fails', async () => {
		configureStepUp(() => Promise.reject(new ApiError('stepup_cancelled', 'Geannuleerd', 0)));
		fetchMock.mockResolvedValueOnce(json(403, { error: 'stepup_required', message: 'Bevestig' }));
		const err = await rejection(api.send('PUT', '/apps/a1', {}));
		expect(err.code).toBe('stepup_cancelled');
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});

	it('throws stepup_required when no step-up hook is configured', async () => {
		fetchMock.mockResolvedValueOnce(json(403, { error: 'stepup_required', message: 'Bevestig' }));
		const err = await rejection(api.send('PUT', '/apps/a1', {}));
		expect(err.code).toBe('stepup_required');
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});
});

describe('401 handling', () => {
	it('redirects to /login only for unauthenticated, not for other 401s', async () => {
		vi.stubGlobal('location', { pathname: '/instellingen/apparaten', assign });
		fetchMock.mockResolvedValueOnce(
			json(401, { error: 'passkey_failed', message: 'The passkey was not recognised.' })
		);
		const err = await rejection(api.send('POST', '/admin/passkeys/finish', {}));
		expect(err.code).toBe('passkey_failed');
		expect(assign).not.toHaveBeenCalled();
		fetchMock.mockResolvedValueOnce(json(401, { error: 'unauthenticated' }));
		await rejection(api.get('/me'));
		expect(assign).toHaveBeenCalledWith('/login');
	});
});

describe('shared step-up', () => {
	it('runs one ceremony for concurrent stepup_required answers', async () => {
		let finish!: () => void;
		const stepUp = vi.fn(() => new Promise<void>((r) => (finish = r)));
		configureStepUp(stepUp);
		fetchMock
			.mockResolvedValueOnce(json(403, { error: 'stepup_required', message: 'Bevestig' }))
			.mockResolvedValueOnce(json(403, { error: 'stepup_required', message: 'Bevestig' }))
			.mockResolvedValueOnce(json(200, { n: 1 }))
			.mockResolvedValueOnce(json(200, { n: 2 }));
		const a = api.send('PUT', '/apps/a', {});
		const b = api.send('PUT', '/apps/b', {});
		await vi.waitFor(() => expect(stepUp).toHaveBeenCalled());
		await new Promise((r) => setTimeout(r, 0));
		finish();
		await expect(Promise.all([a, b])).resolves.toHaveLength(2);
		expect(stepUp).toHaveBeenCalledTimes(1);
		expect(fetchMock).toHaveBeenCalledTimes(4);
	});

	it('starts a new ceremony once the previous one settled', async () => {
		const stepUp = vi.fn().mockResolvedValue(undefined);
		configureStepUp(stepUp);
		for (let i = 0; i < 2; i++) {
			fetchMock
				.mockResolvedValueOnce(json(403, { error: 'stepup_required', message: 'Bevestig' }))
				.mockResolvedValueOnce(json(200, {}));
			await api.send('PUT', '/apps/a', {});
		}
		expect(stepUp).toHaveBeenCalledTimes(2);
	});

	it('shares a failed ceremony and lets the next request try again', async () => {
		const stepUp = vi
			.fn()
			.mockRejectedValueOnce(new ApiError('stepup_cancelled', 'Geannuleerd', 0))
			.mockResolvedValueOnce(undefined);
		configureStepUp(stepUp);
		fetchMock
			.mockResolvedValueOnce(json(403, { error: 'stepup_required', message: 'Bevestig' }))
			.mockResolvedValueOnce(json(403, { error: 'stepup_required', message: 'Bevestig' }));
		const [a, b] = await Promise.all([
			rejection(api.send('PUT', '/apps/a', {})),
			rejection(api.send('PUT', '/apps/b', {}))
		]);
		expect(a.code).toBe('stepup_cancelled');
		expect(b.code).toBe('stepup_cancelled');
		expect(stepUp).toHaveBeenCalledTimes(1);
		fetchMock
			.mockResolvedValueOnce(json(403, { error: 'stepup_required', message: 'Bevestig' }))
			.mockResolvedValueOnce(json(200, {}));
		await api.send('PUT', '/apps/a', {});
		expect(stepUp).toHaveBeenCalledTimes(2);
	});
});

describe('api.blob', () => {
	it('returns the response body as a Blob', async () => {
		fetchMock.mockResolvedValueOnce(
			new Response('{"a":1}\n{"a":2}\n', { status: 200, headers: { 'Content-Type': 'application/x-ndjson' } })
		);
		const b = await api.blob('/admin/audit/export');
		expect(fetchMock.mock.calls[0][0]).toBe('/api/admin/audit/export');
		expect(fetchMock.mock.calls[0][1].credentials).toBe('same-origin');
		expect(await b.text()).toBe('{"a":1}\n{"a":2}\n');
	});

	it('maps errors and sends an expired session to /login', async () => {
		vi.stubGlobal('location', { pathname: '/instellingen/activiteit', assign });
		fetchMock.mockResolvedValueOnce(json(401, { error: 'unauthenticated', message: 'You are not signed in.' }));
		const err = await rejection(api.blob('/admin/audit/export'));
		expect(err).toBeInstanceOf(ApiError);
		expect(err.code).toBe('unauthenticated');
		expect(assign).toHaveBeenCalledWith('/login');
	});
});
