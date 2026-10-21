import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { authenticate, b64urlDecode, b64urlEncode, ceremony, isCreation, passkeyName, register } from './webauthn';

const bytes = (...b: number[]) => new Uint8Array(b).buffer;
const view = (b: ArrayBuffer | ArrayBufferView) =>
	Array.from(b instanceof ArrayBuffer ? new Uint8Array(b) : new Uint8Array(b.buffer, b.byteOffset, b.byteLength));

describe('base64url', () => {
	it('round-trips every byte value and length', () => {
		for (let n = 0; n <= 70; n++) {
			const src = new Uint8Array(n).map((_, i) => (i * 37 + n) & 255);
			const s = b64urlEncode(src);
			expect(s).toMatch(/^[A-Za-z0-9_-]*$/); // URL alphabet, no padding
			expect(view(b64urlDecode(s))).toEqual(Array.from(src));
		}
	});

	it('uses the URL alphabet', () => {
		expect(b64urlEncode(bytes(0xfb, 0xff, 0xbf))).toBe('-_-_');
		expect(b64urlEncode(bytes(1))).toBe('AQ');
	});

	it('accepts padded and standard base64 input', () => {
		expect(view(b64urlDecode('AQ=='))).toEqual([1]);
		expect(view(b64urlDecode('+/+/'))).toEqual([0xfb, 0xff, 0xbf]);
	});
});

// go-webauthn options as served by /begin endpoints.
const creationJSON = {
	publicKey: {
		rp: { name: 'Keystone', id: 'keystone.home.lan' },
		user: { name: 'thijs', displayName: 'Thijs', id: 'AQID' },
		challenge: 'BAUG',
		pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
		timeout: 300000,
		excludeCredentials: [{ type: 'public-key', id: 'CQo' }],
		authenticatorSelection: { requireResidentKey: true, residentKey: 'required', userVerification: 'required' }
	}
};
const requestJSON = {
	publicKey: {
		challenge: 'BAUG',
		timeout: 300000,
		rpId: 'keystone.home.lan',
		allowCredentials: [{ type: 'public-key', id: 'CQo', transports: ['internal'] }],
		userVerification: 'required'
	}
};

let create: ReturnType<typeof vi.fn>;
let get: ReturnType<typeof vi.fn>;

beforeEach(() => {
	create = vi.fn();
	get = vi.fn();
	vi.stubGlobal('navigator', { credentials: { create, get }, userAgent: '' });
	vi.stubGlobal('PublicKeyCredential', function PublicKeyCredential() {});
});
afterEach(() => vi.unstubAllGlobals());

describe('isCreation', () => {
	it('tells registration options from assertion options by publicKey.user', () => {
		expect(isCreation(creationJSON)).toBe(true);
		expect(isCreation(requestJSON)).toBe(false);
	});
});

describe('register', () => {
	it('serializes PublicKeyCredential to JSON expected by go-webauthn', async () => {
		create.mockResolvedValueOnce({
			id: 'CwwN',
			rawId: bytes(11, 12, 13),
			type: 'public-key',
			authenticatorAttachment: 'platform',
			getClientExtensionResults: () => ({ credProps: { rk: true } }),
			response: {
				clientDataJSON: bytes(1, 2),
				attestationObject: bytes(3, 4, 255),
				getTransports: () => ['internal', 'hybrid']
			}
		});

		const out = await register(creationJSON);

		const opts = create.mock.calls[0][0];
		expect(view(opts.publicKey.challenge)).toEqual([4, 5, 6]);
		expect(view(opts.publicKey.user.id)).toEqual([1, 2, 3]);
		expect(view(opts.publicKey.excludeCredentials[0].id)).toEqual([9, 10]);
		expect(opts.publicKey.rp).toEqual({ name: 'Keystone', id: 'keystone.home.lan' });
		expect(opts.publicKey.authenticatorSelection.residentKey).toBe('required');

		expect(out).toEqual({
			id: 'CwwN',
			rawId: 'CwwN',
			type: 'public-key',
			authenticatorAttachment: 'platform',
			clientExtensionResults: { credProps: { rk: true } },
			response: { clientDataJSON: 'AQI', attestationObject: 'AwT_', transports: ['internal', 'hybrid'] }
		});
		expect(JSON.parse(JSON.stringify(out))).toEqual(out);
	});

	it('prefers the native JSON helpers when the browser has them', async () => {
		const parsed = { challenge: bytes(4, 5, 6) };
		const native = vi.fn().mockReturnValue(parsed);
		vi.stubGlobal('PublicKeyCredential', { parseCreationOptionsFromJSON: native });
		const json = { id: 'x', rawId: 'x', type: 'public-key', response: {} };
		create.mockResolvedValueOnce({ toJSON: () => json });

		await expect(register(creationJSON)).resolves.toBe(json);
		expect(native).toHaveBeenCalledWith(creationJSON.publicKey);
		expect(create).toHaveBeenCalledWith({ publicKey: parsed });
	});

	it('rejects request options', async () => {
		await expect(register(requestJSON)).rejects.toThrow();
		expect(create).not.toHaveBeenCalled();
	});
});

describe('authenticate', () => {
	const assertion = {
		id: 'CQo',
		rawId: bytes(9, 10),
		type: 'public-key',
		authenticatorAttachment: null,
		getClientExtensionResults: () => ({}),
		response: {
			clientDataJSON: bytes(1),
			authenticatorData: bytes(2),
			signature: bytes(3),
			userHandle: bytes(7, 7)
		}
	};

	it('decodes allowCredentials and serializes the assertion', async () => {
		get.mockResolvedValueOnce(assertion);
		const out = await authenticate(requestJSON);
		const opts = get.mock.calls[0][0];
		expect(view(opts.publicKey.challenge)).toEqual([4, 5, 6]);
		expect(view(opts.publicKey.allowCredentials[0].id)).toEqual([9, 10]);
		expect(opts.publicKey.allowCredentials[0].transports).toEqual(['internal']);
		expect(opts.mediation).toBeUndefined();
		expect(out).toEqual({
			id: 'CQo',
			rawId: 'CQo',
			type: 'public-key',
			clientExtensionResults: {},
			response: { clientDataJSON: 'AQ', authenticatorData: 'Ag', signature: 'Aw', userHandle: 'Bwc' }
		});
	});

	it('uses conditional mediation with the abort signal when asked', async () => {
		get.mockResolvedValueOnce({ ...assertion, response: { ...assertion.response, userHandle: null } });
		const ctl = new AbortController();
		const out = (await authenticate(requestJSON, { conditional: true, signal: ctl.signal })) as {
			response: Record<string, unknown>;
		};
		const opts = get.mock.calls[0][0];
		expect(opts.mediation).toBe('conditional');
		expect(opts.signal).toBe(ctl.signal);
		expect(out.response.userHandle).toBeUndefined();
	});
});

describe('passkeyName', () => {
	it('names the passkey after the device', () => {
		expect(passkeyName('Mozilla/5.0 (iPhone; CPU iPhone OS 19_0 like Mac OS X)')).toBe('iPhone');
		expect(passkeyName('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)')).toBe('Mac');
		expect(passkeyName('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')).toBe('Windows-pc');
		expect(passkeyName('Mozilla/5.0 (Linux; Android 15; Pixel 9)')).toBe('Android');
		expect(passkeyName('curl/8')).toBe('Passkey');
	});
});

describe('ceremony', () => {
	const ok = (body: unknown) => new Response(JSON.stringify(body), { status: 200 });
	const fakeCred = {
		id: 'AQ',
		rawId: bytes(1),
		type: 'public-key',
		response: { clientDataJSON: bytes(1), attestationObject: bytes(2) }
	};

	it('registers when begin returns creation options and names the passkey', async () => {
		const fetchMock = vi.fn().mockResolvedValueOnce(ok(creationJSON)).mockResolvedValueOnce(ok({ ok: true }));
		vi.stubGlobal('fetch', fetchMock);
		vi.stubGlobal('navigator', { credentials: { create, get }, userAgent: 'Mozilla/5.0 (Macintosh)' });
		create.mockResolvedValueOnce(fakeCred);

		await expect(ceremony('/auth/passkey/begin', '/auth/passkey/finish')).resolves.toEqual({ ok: true });
		expect(fetchMock.mock.calls[0][0]).toBe('/api/auth/passkey/begin');
		expect(fetchMock.mock.calls[1][0]).toBe('/api/auth/passkey/finish?name=Mac');
		expect(JSON.parse(fetchMock.mock.calls[1][1].body).response.attestationObject).toBe('Ag');
		expect(get).not.toHaveBeenCalled();
	});

	it('authenticates when begin returns request options', async () => {
		const fetchMock = vi.fn().mockResolvedValueOnce(ok(requestJSON)).mockResolvedValueOnce(ok({ ok: true }));
		vi.stubGlobal('fetch', fetchMock);
		get.mockResolvedValueOnce({
			...fakeCred,
			response: { clientDataJSON: bytes(1), authenticatorData: bytes(2), signature: bytes(3), userHandle: null }
		});

		await ceremony('/auth/stepup/begin', '/auth/stepup/finish');
		expect(fetchMock.mock.calls[1][0]).toBe('/api/auth/stepup/finish');
		expect(create).not.toHaveBeenCalled();
	});
});
