/** Error thrown for every failed API call; message is Dutch and user-presentable. */
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

/**
 * Registers the passkey step-up ceremony (Task 11 wires WebAuthn). When a
 * write answers 403 stepup_required, the ceremony runs and the request is
 * retried once. Pass null to unregister.
 */
export function configureStepUp(fn: StepUp | null): void {
	stepUp = fn;
}

const NO_REDIRECT = ['/login', '/setup'];

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
	const init: RequestInit & { headers: Record<string, string> } = {
		method,
		credentials: 'same-origin',
		headers: { Accept: 'application/json' }
	};
	if (method !== 'GET') {
		init.headers['Content-Type'] = 'application/json';
		if (body !== undefined) init.body = JSON.stringify(body);
	}
	let res: Response;
	try {
		res = await fetch('/api' + path, init);
	} catch {
		throw new ApiError('network', 'Keystone is niet bereikbaar. Controleer je verbinding.', 0);
	}
	if (res.ok) {
		const text = await res.text();
		return (text ? JSON.parse(text) : undefined) as T;
	}
	let code = `http_${res.status}`;
	let message = `Er ging iets mis bij Keystone (${res.status}).`;
	let data: Record<string, unknown> = {};
	try {
		const body = await res.json();
		if (body && typeof body === 'object') data = body;
		if (typeof body?.error === 'string') code = body.error;
		if (typeof body?.message === 'string' && body.message) message = body.message;
	} catch {
		/* non-JSON body: keep the generic error */
	}
	// Sign-in and setup answer 401 for wrong codes/passwords; they handle it themselves.
	if (res.status === 401 && !NO_REDIRECT.includes(location.pathname)) location.assign('/login');
	throw new ApiError(code, message, res.status, data);
}

async function withStepUp<T>(method: string, path: string, body?: unknown): Promise<T> {
	try {
		return await request<T>(method, path, body);
	} catch (e) {
		if (!(e instanceof ApiError) || e.code !== 'stepup_required' || !stepUp) throw e;
		await stepUp();
		return request<T>(method, path, body);
	}
}

export const api = {
	get: <T>(path: string) => withStepUp<T>('GET', path),
	send: <T>(method: 'POST' | 'PUT' | 'PATCH' | 'DELETE', path: string, body?: unknown) =>
		withStepUp<T>(method, path, body)
};

/** GET /api/me */
export interface Me {
	username: string;
	display_name: string | null;
	given_name: string | null;
	email: string | null;
	emergency: boolean;
	must_register_passkey: boolean;
	needs_stepup: boolean;
}
