import { describe, expect, it } from 'vitest';
import { adWeakenings, type ADSecurity } from './adSecurity';

const safe: ADSecurity = {
	url: 'ldaps://dc.home.lan',
	start_tls: false,
	insecure_skip_verify: false,
	allow_plaintext: false,
	min_tls: '1.2'
};
const titles = (a: ADSecurity | null, b: ADSecurity) => adWeakenings(a, b).map((w) => w.title);

describe('adWeakenings', () => {
	it('is empty for unchanged or stricter settings', () => {
		expect(adWeakenings(safe, safe)).toEqual([]);
		expect(adWeakenings({ ...safe, insecure_skip_verify: true }, safe)).toEqual([]);
		expect(adWeakenings(safe, { ...safe, min_tls: '1.3' })).toEqual([]);
		const tls = { ...safe, url: 'ldap://dc.home.lan', start_tls: true };
		expect(adWeakenings(safe, tls)).toEqual([]);
	});

	it('flags turning on skip-verify and plaintext', () => {
		expect(titles(safe, { ...safe, insecure_skip_verify: true })).toEqual(['Certificaat niet controleren']);
		expect(titles(safe, { ...safe, allow_plaintext: true })).toEqual(['Onversleuteld ldap:// toestaan']);
	});

	it('flags turning off StartTLS on an ldap:// URL only', () => {
		const tls = { ...safe, url: 'ldap://dc.home.lan', start_tls: true };
		expect(titles(tls, { ...tls, start_tls: false })).toEqual(['StartTLS gebruiken']);
		const ldaps = { ...safe, start_tls: true };
		expect(adWeakenings(ldaps, { ...ldaps, start_tls: false })).toEqual([]);
	});

	it('flags switching to ldap:// without StartTLS', () => {
		const w = adWeakenings(safe, { ...safe, url: ' LDAP://dc.home.lan' });
		expect(w.map((x) => x.title)).toEqual(['Server']);
		expect(w[0].detail).toBe('ldaps://dc.home.lan → LDAP://dc.home.lan');
	});

	it('flags a lower minimum TLS version', () => {
		expect(titles({ ...safe, min_tls: '1.3' }, safe)).toEqual(['Minimale TLS-versie']);
	});

	it('treats a first configuration as starting from safe defaults', () => {
		expect(titles(null, { ...safe, url: 'ldap://dc', allow_plaintext: true })).toEqual([
			'Onversleuteld ldap:// toestaan',
			'Server'
		]);
	});
});
