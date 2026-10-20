/** Error thrown for every failed API call; message is Dutch and user-presentable. */
export class ApiError extends Error {
	constructor(
		public code: string,
		message: string,
		public status: number
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
	try {
		const data = await res.json();
		if (typeof data?.error === 'string') code = data.error;
		if (typeof data?.message === 'string' && data.message) message = data.message;
	} catch {
		/* non-JSON body: keep the generic error */
	}
	if (res.status === 401 && location.pathname !== '/login') location.assign('/login');
	throw new ApiError(code, message, res.status);
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
