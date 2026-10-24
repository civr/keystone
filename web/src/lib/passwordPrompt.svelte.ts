/**
 * Step-up without a passkey: a sheet asks for the password (StepUpPassword)
 * and api.ts retries the request once it is confirmed.
 */
export const prompt = $state<{ open: boolean }>({ open: false });

let pending: { resolve: () => void; reject: (e: Error) => void } | null = null;

/** Opens the sheet; resolves once the password is confirmed. */
export function askPassword(): Promise<void> {
	pending?.reject(new Error('replaced'));
	prompt.open = true;
	return new Promise((resolve, reject) => (pending = { resolve, reject }));
}

/** Called by the sheet. */
export function settlePrompt(ok: boolean): void {
	const p = pending;
	pending = null;
	prompt.open = false;
	if (ok) p?.resolve();
	else p?.reject(new Error('cancelled'));
}
