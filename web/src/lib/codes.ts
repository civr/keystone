/** Formats a setup or recovery code as XXXX-XXXX-XXXX-XXXX while typing. */
export function formatCode(raw: string): string {
	const s = raw.toUpperCase().replace(/[^0-9A-Z]/g, '').slice(0, 16);
	return s.match(/.{1,4}/g)?.join('-') ?? '';
}

/** Text of keystone-recovery-codes.txt. */
export function codesFile(codes: string[], site: string, at: Date = new Date()): string {
	const date = new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'long', year: 'numeric' }).format(at);
	return [
		'Keystone recovery codes',
		site,
		`Made on ${date}`,
		'',
		'Each code works once. Use one if you lose your passkey:',
		'sign in with your password and choose "Use a recovery code".',
		'',
		...codes.map((c, i) => `${String(i + 1).padStart(3)}. ${c}`),
		''
	].join('\n');
}

/** Lets the browser save text as a file. */
export function download(name: string, text: string): void {
	const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }));
	const a = Object.assign(document.createElement('a'), { href: url, download: name });
	document.body.append(a);
	a.click();
	a.remove();
	setTimeout(() => URL.revokeObjectURL(url), 1000);
}
