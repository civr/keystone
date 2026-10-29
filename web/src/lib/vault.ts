/** GET /api/vault item. */
export interface VaultItem {
	id: string;
	title: string;
	url: string;
	username: string;
	has_password: boolean;
	has_note: boolean;
	kind: VaultKind;
	/** Provider id when the secret lives in 1Password. */
	provider?: string;
	updated_at: string;
}

export type VaultKind = 'login' | 'api_key' | 'token' | 'client_secret' | 'note';

/** Kinds of secret, with how their fields are named. */
export const KINDS: { id: VaultKind; label: string; user: string | null; secret: string }[] = [
	{ id: 'login', label: 'Login', user: 'Username', secret: 'Password' },
	{ id: 'api_key', label: 'API key', user: null, secret: 'Key' },
	{ id: 'token', label: 'Token', user: null, secret: 'Token' },
	{ id: 'client_secret', label: 'Client secret', user: 'Client ID', secret: 'Client secret' },
	{ id: 'note', label: 'Secure note', user: null, secret: '' }
];

export function kindOf(id: string | undefined) {
	return KINDS.find((k) => k.id === id) ?? KINDS[0];
}

/** Items whose title, username or address contains query (case-insensitive). */
export function filterItems(items: VaultItem[], query: string): VaultItem[] {
	const q = query.trim().toLowerCase();
	if (!q) return items;
	return items.filter((i) => [i.title, i.username, i.url].some((v) => v.toLowerCase().includes(q)));
}

const LOWER = 'abcdefghijkmnopqrstuvwxyz';
const UPPER = 'ABCDEFGHJKLMNPQRSTUVWXYZ';
const DIGITS = '23456789';

function pick(set: string, rand: (n: number) => number): string {
	return set[rand(set.length)];
}

function cryptoRand(n: number): number {
	// Rejection sampling: no modulo bias.
	const limit = Math.floor(0x100000000 / n) * n;
	const buf = new Uint32Array(1);
	do crypto.getRandomValues(buf);
	while (buf[0] >= limit);
	return buf[0] % n;
}

/**
 * A strong password in Apple's style: three groups of six separated by
 * hyphens ("kexdyb-3pujxo-Gamwyq"), with one capital and one digit.
 */
export function strongPassword(rand: (n: number) => number = cryptoRand): string {
	const chars = Array.from({ length: 18 }, () => pick(LOWER, rand));
	const upperAt = rand(18);
	let digitAt = rand(17);
	if (digitAt >= upperAt) digitAt++;
	chars[upperAt] = pick(UPPER, rand);
	chars[digitAt] = pick(DIGITS, rand);
	return [0, 6, 12].map((i) => chars.slice(i, i + 6).join('')).join('-');
}
