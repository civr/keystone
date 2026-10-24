/** The AD connection fields that decide how well the bind is protected. */
export interface ADSecurity {
	url: string;
	start_tls: boolean;
	insecure_skip_verify: boolean;
	allow_plaintext: boolean;
	min_tls: string;
}

const DEFAULTS: ADSecurity = { url: '', start_tls: false, insecure_skip_verify: false, allow_plaintext: false, min_tls: '1.2' };

const onOff = (v: boolean) => (v ? 'On' : 'Off');
const isPlainLDAP = (url: string) => url.trim().toLowerCase().startsWith('ldap://');
/** ldap:// without StartTLS: the bind travels unencrypted. */
const unprotected = (c: ADSecurity) => isPlainLDAP(c.url) && !c.start_tls;

/**
 * The changes from saved to next that lower the security of the AD
 * connection, as confirm-sheet items ("This lowers security"). A first
 * configuration (saved null) is compared with the safe defaults.
 */
export function adWeakenings(saved: ADSecurity | null, next: ADSecurity): { title: string; detail: string }[] {
	const prev = saved ?? DEFAULTS;
	const out: { title: string; detail: string }[] = [];
	if (!prev.insecure_skip_verify && next.insecure_skip_verify)
		out.push({ title: 'Do not check the certificate', detail: 'Off → On' });
	if (!prev.allow_plaintext && next.allow_plaintext)
		out.push({ title: 'Allow unencrypted ldap://', detail: 'Off → On' });
	if (!unprotected(prev) && unprotected(next)) {
		if (isPlainLDAP(prev.url) && prev.start_tls)
			out.push({ title: 'Use StartTLS', detail: `${onOff(true)} → ${onOff(false)}` });
		else out.push({ title: 'Server', detail: `${prev.url.trim() || 'empty'} → ${next.url.trim()}` });
	}
	if (prev.min_tls === '1.3' && next.min_tls !== '1.3')
		out.push({ title: 'Minimum TLS version', detail: `TLS ${prev.min_tls} → TLS ${next.min_tls}` });
	return out;
}
