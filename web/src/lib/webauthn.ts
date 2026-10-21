/**
 * WebAuthn for go-webauthn: options arrive as JSON ({publicKey: {...}} with
 * base64url binary fields) and credentials go back as JSON with base64url
 * fields. Uses the browser's native JSON helpers when present, with a small
 * manual fallback (no library).
 */
import { api } from './api';

type Bytes = ArrayBuffer | ArrayBufferView;
type Json = Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any

/** Options JSON as served by a /begin endpoint. */
export interface OptionsJSON {
	publicKey: Json;
	mediation?: string;
}

function u8(b: Bytes): Uint8Array {
	return b instanceof ArrayBuffer ? new Uint8Array(b) : new Uint8Array(b.buffer, b.byteOffset, b.byteLength);
}

export function b64urlEncode(b: Bytes): string {
	let s = '';
	for (const c of u8(b)) s += String.fromCharCode(c);
	return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

/** Decodes base64url (or standard base64), padded or not. */
export function b64urlDecode(s: string): ArrayBuffer {
	const std = s.replace(/-/g, '+').replace(/_/g, '/').replace(/=+$/, '');
	const bin = atob(std + '='.repeat((4 - (std.length % 4)) % 4));
	const out = new Uint8Array(bin.length);
	for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
	return out.buffer;
}

/** Registration options carry a user; assertion options do not. */
export function isCreation(opts: OptionsJSON): boolean {
	return !!opts?.publicKey?.user;
}

const decodeList = (list?: Json[]) => list?.map((c) => ({ ...c, id: b64urlDecode(c.id) }));

// The static JSON parsers exist in recent browsers only.
type PKCStatics = {
	parseCreationOptionsFromJSON?: (j: Json) => PublicKeyCredentialCreationOptions;
	parseRequestOptionsFromJSON?: (j: Json) => PublicKeyCredentialRequestOptions;
};
const statics = (): PKCStatics =>
	((globalThis as unknown as { PublicKeyCredential?: PKCStatics }).PublicKeyCredential ?? {});

function creationOptions(pk: Json): PublicKeyCredentialCreationOptions {
	const native = statics().parseCreationOptionsFromJSON;
	if (native) return native(pk);
	return {
		...pk,
		challenge: b64urlDecode(pk.challenge),
		user: { ...pk.user, id: b64urlDecode(pk.user.id) },
		excludeCredentials: decodeList(pk.excludeCredentials)
	} as PublicKeyCredentialCreationOptions;
}

function requestOptions(pk: Json): PublicKeyCredentialRequestOptions {
	const native = statics().parseRequestOptionsFromJSON;
	if (native) return native(pk);
	return {
		...pk,
		challenge: b64urlDecode(pk.challenge),
		allowCredentials: decodeList(pk.allowCredentials)
	} as PublicKeyCredentialRequestOptions;
}

/** Serializes a credential the way go-webauthn's parsers expect. */
function credentialJSON(cred: PublicKeyCredential): Json {
	const c = cred as PublicKeyCredential & { toJSON?: () => Json };
	if (typeof c.toJSON === 'function') return c.toJSON();
	const r = c.response as AuthenticatorAttestationResponse & AuthenticatorAssertionResponse;
	const response: Json = { clientDataJSON: b64urlEncode(r.clientDataJSON) };
	if (r.attestationObject) {
		response.attestationObject = b64urlEncode(r.attestationObject);
		if (typeof r.getTransports === 'function') response.transports = r.getTransports();
	} else {
		response.authenticatorData = b64urlEncode(r.authenticatorData);
		response.signature = b64urlEncode(r.signature);
		if (r.userHandle) response.userHandle = b64urlEncode(r.userHandle);
	}
	const out: Json = {
		id: c.id,
		rawId: b64urlEncode(c.rawId),
		type: c.type,
		clientExtensionResults: c.getClientExtensionResults?.() ?? {},
		response
	};
	if (c.authenticatorAttachment) out.authenticatorAttachment = c.authenticatorAttachment;
	return out;
}

export function supported(): boolean {
	return typeof globalThis.PublicKeyCredential !== 'undefined' && !!navigator.credentials;
}

/** True when the browser can offer passkeys in the username autofill. */
export async function conditionalSupported(): Promise<boolean> {
	const pkc = globalThis.PublicKeyCredential as { isConditionalMediationAvailable?: () => Promise<boolean> } | undefined;
	try {
		return !!(await pkc?.isConditionalMediationAvailable?.());
	} catch {
		return false;
	}
}

/** Creates a passkey from registration options; returns the finish body. */
export async function register(opts: OptionsJSON): Promise<Json> {
	if (!isCreation(opts)) throw new Error('webauthn: registration options expected');
	const cred = await navigator.credentials.create({ publicKey: creationOptions(opts.publicKey) });
	if (!cred) throw new DOMException('Geen passkey aangemaakt.', 'NotAllowedError');
	return credentialJSON(cred as PublicKeyCredential);
}

/**
 * Signs a challenge with a passkey; returns the finish body. conditional
 * offers the passkey in the username autofill instead of a modal prompt.
 */
export async function authenticate(
	opts: OptionsJSON,
	{ conditional = false, signal }: { conditional?: boolean; signal?: AbortSignal } = {}
): Promise<Json> {
	const req: CredentialRequestOptions = { publicKey: requestOptions(opts.publicKey) };
	if (conditional) req.mediation = 'conditional' as CredentialMediationRequirement;
	if (signal) req.signal = signal;
	const cred = await navigator.credentials.get(req);
	if (!cred) throw new DOMException('Geen passkey gekozen.', 'NotAllowedError');
	return credentialJSON(cred as PublicKeyCredential);
}

/** A default passkey name for this device. */
export function passkeyName(ua: string = navigator.userAgent): string {
	if (/iPhone/.test(ua)) return 'iPhone';
	if (/iPad/.test(ua)) return 'iPad';
	if (/Android/.test(ua)) return 'Android';
	if (/Macintosh|Mac OS X/.test(ua)) return 'Mac';
	if (/Windows/.test(ua)) return 'Windows-pc';
	if (/Linux|CrOS/.test(ua)) return 'Linux';
	return 'Passkey';
}

/**
 * One full ceremony: POST begin, create or use a passkey (decided by the
 * options), POST the result to finish (registrations get ?name=).
 */
export async function ceremony<T = unknown>(
	begin: string,
	finish: string,
	{ conditional = false, signal }: { conditional?: boolean; signal?: AbortSignal } = {}
): Promise<T> {
	const opts = await api.send<OptionsJSON>('POST', begin);
	if (isCreation(opts)) {
		const body = await register(opts);
		return api.send<T>('POST', `${finish}?name=${encodeURIComponent(passkeyName())}`, body);
	}
	return api.send<T>('POST', finish, await authenticate(opts, { conditional, signal }));
}

/** Readable Dutch text for a failed passkey prompt. */
export function passkeyError(e: unknown): string {
	const name = (e as { name?: string })?.name;
	if (name === 'NotAllowedError' || name === 'AbortError') return 'De passkey-vraag is geannuleerd of verlopen.';
	if (name === 'InvalidStateError') return 'Er staat op dit apparaat al een passkey voor Keystone.';
	if (name === 'SecurityError') return 'Passkeys werken alleen op het publieke adres van Keystone (https).';
	if (name === 'NotSupportedError') return 'Passkeys worden niet ondersteund in deze browser.';
	if (name === 'ApiError') return (e as Error).message;
	return 'De passkey-stap is mislukt. Probeer het opnieuw.';
}
