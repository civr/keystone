/** Error thrown for every failed API call; message is user-presentable. */
export class ApiError extends Error {
	constructor(
		public code: string,
		message: string,
		public status: number,
		/** The full JSON error body, e.g. retry_after or emergency. */
		public data: Record<string, unknown> = {}
	) {
		super(message);
		this.name = 'ApiError';
	}
}

type StepUp = () => Promise<void>;
let stepUp: StepUp | null = null;
/** The running ceremony, shared by every request that hit stepup_required meanwhile. */
let stepUpInFlight: Promise<void> | null = null;

/**
 * Registers the passkey step-up ceremony (Task 11 wires WebAuthn). When a
 * write answers 403 stepup_required, the ceremony runs and the request is
 * retried once. Pass null to unregister.
 */
export function configureStepUp(fn: StepUp | null): void {
	stepUp = fn;
}

const NO_REDIRECT = ['/login', '/setup'];

async function request<T>(method: string, path: string, body?: unknown, asBlob = false): Promise<T> {
	const init: RequestInit & { headers: Record<string, string> } = {
		method,
		credentials: 'same-origin',
		headers: { Accept: asBlob ? '*/*' : 'application/json' }
	};
	if (method !== 'GET') {
		init.headers['Content-Type'] = 'application/json';
		if (body !== undefined) init.body = JSON.stringify(body);
	}
	let res: Response;
	try {
		res = await fetch('/api' + path, init);
	} catch {
		throw new ApiError('network', 'Keystone cannot be reached. Check your connection.', 0);
	}
	if (res.ok) {
		if (asBlob) return (await res.blob()) as T;
		const text = await res.text();
		return (text ? JSON.parse(text) : undefined) as T;
	}
	let code = `http_${res.status}`;
	let message = `Something went wrong in Keystone (${res.status}).`;
	let data: Record<string, unknown> = {};
	try {
		const body = await res.json();
		if (body && typeof body === 'object') data = body;
		if (typeof body?.error === 'string') code = body.error;
		if (typeof body?.message === 'string' && body.message) message = body.message;
	} catch {
		/* non-JSON body: keep the generic error */
	}
	// Only a missing/expired session goes to /login; other 401s (wrong setup
	// code, passkey not recognised, …) are shown by the page itself.
	if (res.status === 401 && code === 'unauthenticated' && !NO_REDIRECT.includes(location.pathname))
		location.assign('/login');
	throw new ApiError(code, message, res.status, data);
}

/** Runs the step-up ceremony, joining one that is already running. */
function runStepUp(fn: StepUp): Promise<void> {
	stepUpInFlight ??= fn().finally(() => (stepUpInFlight = null));
	return stepUpInFlight;
}

async function withStepUp<T>(method: string, path: string, body?: unknown, asBlob = false): Promise<T> {
	try {
		return await request<T>(method, path, body, asBlob);
	} catch (e) {
		if (!(e instanceof ApiError) || e.code !== 'stepup_required' || !stepUp) throw e;
		await runStepUp(stepUp);
		return request<T>(method, path, body, asBlob);
	}
}

export const api = {
	get: <T>(path: string) => withStepUp<T>('GET', path),
	send: <T>(method: 'POST' | 'PUT' | 'PATCH' | 'DELETE', path: string, body?: unknown) =>
		withStepUp<T>(method, path, body),
	/** GET a non-JSON body (e.g. a download) with the same error and 401 handling. */
	blob: (path: string) => withStepUp<Blob>('GET', path, undefined, true)
};

/** GET /api/me */
export interface Me {
	/** Account id: "owner" or a uuid. */
	id: string;
	role: 'owner' | 'admin' | 'member';
	admin: boolean;
	username: string;
	display_name: string | null;
	given_name: string | null;
	email: string | null;
	emergency: boolean;
	must_register_passkey: boolean;
	needs_stepup: boolean;
	/** No passkey registered yet (setup went on without https). */
	passkey_missing?: boolean;
	/** The client address as Keystone resolved it (after trusted proxies). */
	client_ip: string;
}
