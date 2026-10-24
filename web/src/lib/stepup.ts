import { configureStepUp } from './api';
import { askPassword } from './passwordPrompt.svelte';
import { ceremony } from './webauthn';

/**
 * Wires the step-up into api.ts (Ruling R5): the passkey, or the password
 * while no passkey exists (Keystone used over http, or set up without one).
 */
export function enableStepUp(): void {
	configureStepUp(async () => {
		const me = await fetch('/api/me', { credentials: 'same-origin' })
			.then((r) => (r.ok ? r.json() : null))
			.catch(() => null);
		if (me?.passkey_missing) return askPassword();
		await ceremony('/auth/stepup/begin', '/auth/stepup/finish');
	});
}
