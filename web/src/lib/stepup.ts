import { configureStepUp } from './api';
import { ceremony } from './webauthn';

/** Wires the passkey step-up into api.ts (Ruling R5). */
export function enableStepUp(): void {
	configureStepUp(() => ceremony('/auth/stepup/begin', '/auth/stepup/finish').then(() => undefined));
}
